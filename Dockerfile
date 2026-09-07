# Multi-stage build for go-emailservice-ads
FROM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build service and operational tools
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/goemailservices ./cmd/goemailservices
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailctl ./cmd/mailctl
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailflow-probe ./cmd/mailflow-probe
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-backup ./cmd/mailhub-backup
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-privacy ./cmd/mailhub-privacy
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-failover ./cmd/mailhub-failover
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-ha-check ./cmd/mailhub-ha-check
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-log ./cmd/mailhub-log
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-preserve ./cmd/mailhub-preserve
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /build/bin/mailhub-authcheck ./cmd/mailhub-authcheck

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata bash curl aws-cli

# Create non-root user (mailservice group/user to avoid conflict with existing mail group)
RUN addgroup -g 1000 mailservice && \
    adduser -D -u 1000 -G mailservice mailservice

# Set working directory
WORKDIR /opt/goemailservices

# Copy binaries from builder
COPY --from=builder /build/bin/goemailservices /usr/local/bin/
COPY --from=builder /build/bin/mailctl /usr/local/bin/
COPY --from=builder /build/bin/mailflow-probe /usr/local/bin/
COPY --from=builder /build/bin/mailhub-backup /usr/local/bin/
COPY --from=builder /build/bin/mailhub-privacy /usr/local/bin/
COPY --from=builder /build/bin/mailhub-failover /usr/local/bin/
COPY --from=builder /build/bin/mailhub-ha-check /usr/local/bin/
COPY --from=builder /build/bin/mailhub-log /usr/local/bin/
COPY --from=builder /build/bin/mailhub-preserve /usr/local/bin/
COPY --from=builder /build/bin/mailhub-authcheck /usr/local/bin/

# Copy default configuration
COPY config.yaml /opt/goemailservices/config.yaml
COPY policies.yaml /opt/goemailservices/policies.yaml
COPY examples/policies /opt/goemailservices/examples/policies

# Create data directories
RUN mkdir -p /data /var/lib/mail-storage /var/log/mail && \
    chown -R mailservice:mailservice /data /var/lib/mail-storage /var/log/mail /opt/goemailservices

# Switch to non-root user
USER mailservice

# Expose ports
EXPOSE 2525 8080 50051 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Environment variables
ENV MAILHUB_DATA_DIR=/data \
    LOG_LEVEL=info \
    SMTP_ADDR=:2525 \
    API_REST_ADDR=:8080 \
    API_GRPC_ADDR=:50051

# Default command
ENTRYPOINT ["/usr/local/bin/goemailservices"]
CMD ["--config", "/opt/goemailservices/config.yaml"]
