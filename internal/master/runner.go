package master

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ServiceRunner manages a single service instance
type ServiceRunner struct {
	name     string
	service  *Service
	logger   *zap.Logger
	listener net.Listener
	ctx      context.Context

	// Connection tracking
	activeConns int64
	totalConns  int64

	// Worker management
	wg sync.WaitGroup
}

// Run starts the service and begins accepting connections
func (sr *ServiceRunner) Run() error {
	sr.logger.Info("Service runner starting",
		zap.String("type", sr.service.Type),
		zap.String("listen", sr.service.Listen),
		zap.Int("workers", sr.service.Workers))

	// Accept connections
	for {
		select {
		case <-sr.ctx.Done():
			sr.logger.Info("Service runner context cancelled")
			return nil

		default:
			conn, err := sr.listener.Accept()
			if err != nil {
				// Check if listener was closed
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					sr.logger.Info("Listener closed, stopping service")
					return nil
				}

				sr.logger.Error("Failed to accept connection", zap.Error(err))
				continue
			}

			// Check connection limits
			maxConns := sr.service.Settings.MaxConnections
			if maxConns > 0 && atomic.LoadInt64(&sr.activeConns) >= int64(maxConns) {
				sr.logger.Warn("Connection limit reached, rejecting connection",
					zap.Int64("active", sr.activeConns),
					zap.Int("max", maxConns))
				conn.Close()
				continue
			}

			// Handle connection in worker pool
			sr.wg.Add(1)
			atomic.AddInt64(&sr.activeConns, 1)
			atomic.AddInt64(&sr.totalConns, 1)

			go sr.handleConnection(conn)
		}
	}
}

// handleConnection processes a single client connection
func (sr *ServiceRunner) handleConnection(conn net.Conn) {
	defer sr.wg.Done()
	defer atomic.AddInt64(&sr.activeConns, -1)
	defer conn.Close()

	sr.logger.Debug("New connection",
		zap.String("remote_addr", conn.RemoteAddr().String()),
		zap.String("service_type", sr.service.Type))

	// This is a placeholder - actual protocol handling should be injected
	// For now, just log and close
	// TODO: Integrate with actual SMTP/IMAP/JMAP handlers

	switch sr.service.Type {
	case "smtp":
		sr.handleSMTP(conn)
	case "imap":
		sr.handleIMAP(conn)
	case "jmap":
		sr.handleJMAP(conn)
	default:
		sr.logger.Warn("Unknown service type", zap.String("type", sr.service.Type))
	}
}

// handleSMTP processes SMTP protocol (placeholder)
func (sr *ServiceRunner) handleSMTP(conn net.Conn) {
	// TODO: Integrate with internal/smtpd package
	sr.logger.Debug("SMTP connection handling not yet implemented")
}

// handleIMAP processes IMAP protocol (placeholder)
func (sr *ServiceRunner) handleIMAP(conn net.Conn) {
	// TODO: Integrate with internal/imap package
	sr.logger.Debug("IMAP connection handling not yet implemented")
}

// handleJMAP processes JMAP protocol (placeholder)
func (sr *ServiceRunner) handleJMAP(conn net.Conn) {
	// TODO: Integrate with internal/jmap package
	sr.logger.Debug("JMAP connection handling not yet implemented")
}

// Stop gracefully stops the service runner
func (sr *ServiceRunner) Stop() error {
	sr.logger.Info("Stopping service runner")

	// Wait for active connections to finish with timeout
	done := make(chan struct{})
	go func() {
		sr.wg.Wait()
		close(done)
	}()

	// Wait with timeout (30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case <-done:
		sr.logger.Info("Service runner stopped gracefully",
			zap.Int64("total_connections", sr.totalConns))
		return nil
	case <-ctx.Done():
		sr.logger.Warn("Service runner stop timeout",
			zap.Int64("active_connections", sr.activeConns))
		return fmt.Errorf("stop timeout with %d active connections", sr.activeConns)
	}
}

// GetStats returns service runner statistics
func (sr *ServiceRunner) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"name":              sr.name,
		"type":              sr.service.Type,
		"listen":            sr.service.Listen,
		"active_connections": atomic.LoadInt64(&sr.activeConns),
		"total_connections":  atomic.LoadInt64(&sr.totalConns),
		"workers":            sr.service.Workers,
	}
}
