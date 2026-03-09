package elasticsearch

import (
	"bufio"
	"bytes"
	"net"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

// HeaderLogger handles header extraction and filtering based on configuration
type HeaderLogger struct {
	config         *config.Config
	logger         *zap.Logger
	redactPatterns []*regexp.Regexp
	allowIPNets    []*net.IPNet
	denyIPNets     []*net.IPNet
}

// NewHeaderLogger creates a new header logger
func NewHeaderLogger(cfg *config.Config, logger *zap.Logger) *HeaderLogger {
	hl := &HeaderLogger{
		config: cfg,
		logger: logger,
	}

	// Compile redaction patterns
	for _, pattern := range cfg.Elasticsearch.HeaderLogging.RedactPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			logger.Error("Invalid redaction pattern",
				zap.String("pattern", pattern),
				zap.Error(err))
			continue
		}
		hl.redactPatterns = append(hl.redactPatterns, re)
	}

	// Parse IP allow/deny lists
	hl.allowIPNets = parseIPList(cfg.Elasticsearch.HeaderLogging.AllowIPs, logger)
	hl.denyIPNets = parseIPList(cfg.Elasticsearch.HeaderLogging.DenyIPs, logger)

	return hl
}

// ShouldLogHeaders determines if headers should be logged for this event
func (hl *HeaderLogger) ShouldLogHeaders(event *MailEvent) bool {
	cfg := hl.config.Elasticsearch.HeaderLogging

	// Global toggle
	if !cfg.Enabled {
		return false
	}

	// Check deny lists first (they take precedence)
	if hl.isDenied(event) {
		return false
	}

	// If we have allow lists, check if this event matches
	if hl.hasAllowLists() {
		return hl.isAllowed(event)
	}

	// No specific allow lists, so allow by default
	return true
}

// isDenied checks if the event matches any deny list
func (hl *HeaderLogger) isDenied(event *MailEvent) bool {
	cfg := hl.config.Elasticsearch.HeaderLogging

	// Check domain deny list
	if len(cfg.DenyDomains) > 0 {
		for _, email := range append([]string{event.Envelope.From}, event.Envelope.To...) {
			domain := extractDomain(email)
			if contains(cfg.DenyDomains, domain) {
				return true
			}
		}
	}

	// Check IP deny list
	if len(hl.denyIPNets) > 0 && event.Metadata.ClientIP != "" {
		ip := net.ParseIP(event.Metadata.ClientIP)
		if ip != nil {
			for _, ipNet := range hl.denyIPNets {
				if ipNet.Contains(ip) {
					return true
				}
			}
		}
	}

	// Check MX deny list
	if len(cfg.DenyMXs) > 0 && event.Delivery.RemoteHost != "" {
		if contains(cfg.DenyMXs, event.Delivery.RemoteHost) {
			return true
		}
	}

	return false
}

// isAllowed checks if the event matches any allow list
func (hl *HeaderLogger) isAllowed(event *MailEvent) bool {
	cfg := hl.config.Elasticsearch.HeaderLogging

	// Check domain allow list
	if len(cfg.AllowDomains) > 0 {
		for _, email := range append([]string{event.Envelope.From}, event.Envelope.To...) {
			domain := extractDomain(email)
			if contains(cfg.AllowDomains, domain) {
				return true
			}
		}
	}

	// Check IP allow list
	if len(hl.allowIPNets) > 0 && event.Metadata.ClientIP != "" {
		ip := net.ParseIP(event.Metadata.ClientIP)
		if ip != nil {
			for _, ipNet := range hl.allowIPNets {
				if ipNet.Contains(ip) {
					return true
				}
			}
		}
	}

	// Check MX allow list
	if len(cfg.AllowMXs) > 0 && event.Delivery.RemoteHost != "" {
		if contains(cfg.AllowMXs, event.Delivery.RemoteHost) {
			return true
		}
	}

	return false
}

// hasAllowLists checks if any allow lists are configured
func (hl *HeaderLogger) hasAllowLists() bool {
	cfg := hl.config.Elasticsearch.HeaderLogging
	return len(cfg.AllowDomains) > 0 ||
		len(cfg.AllowIPs) > 0 ||
		len(cfg.AllowMXs) > 0
}

// ExtractHeaders extracts and filters headers from raw message data
func (hl *HeaderLogger) ExtractHeaders(data []byte) (map[string][]string, error) {
	// Parse message to extract headers
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		// If we can't parse as email, try to extract raw headers
		return hl.extractRawHeaders(data)
	}

	headers := make(map[string][]string)

	// Convert mail.Header to textproto.MIMEHeader for easier iteration
	mimeHeader := textproto.MIMEHeader(msg.Header)

	for key := range mimeHeader {
		// Check if we should include this header
		if !hl.shouldIncludeHeader(key) {
			continue
		}

		values := mimeHeader[key]

		// Apply redaction to header values
		redactedValues := make([]string, len(values))
		for i, value := range values {
			redactedValues[i] = hl.redactValue(value)
		}

		headers[key] = redactedValues
	}

	return headers, nil
}

// extractRawHeaders attempts to extract headers from raw data
func (hl *HeaderLogger) extractRawHeaders(data []byte) (map[string][]string, error) {
	headers := make(map[string][]string)

	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	mimeHeader, err := reader.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}

	for key := range mimeHeader {
		if !hl.shouldIncludeHeader(key) {
			continue
		}

		values := mimeHeader[key]
		redactedValues := make([]string, len(values))
		for i, value := range values {
			redactedValues[i] = hl.redactValue(value)
		}

		headers[key] = redactedValues
	}

	return headers, nil
}

// shouldIncludeHeader checks if a header should be included
func (hl *HeaderLogger) shouldIncludeHeader(headerName string) bool {
	cfg := hl.config.Elasticsearch.HeaderLogging

	// Always exclude certain headers
	for _, excluded := range cfg.ExcludeHeaders {
		if strings.EqualFold(headerName, excluded) {
			return false
		}
	}

	// If logging all headers, include it
	if cfg.LogAllHeaders {
		return true
	}

	// Otherwise, only include if it's in the include list
	for _, included := range cfg.IncludeHeaders {
		if strings.EqualFold(headerName, included) {
			return true
		}
	}

	return false
}

// redactValue applies redaction patterns to a header value
func (hl *HeaderLogger) redactValue(value string) string {
	result := value
	for _, pattern := range hl.redactPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

// Helper functions

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func contains(list []string, item string) bool {
	itemLower := strings.ToLower(item)
	for _, element := range list {
		if strings.EqualFold(element, itemLower) {
			return true
		}
	}
	return false
}

func parseIPList(ipList []string, logger *zap.Logger) []*net.IPNet {
	var nets []*net.IPNet

	for _, ipStr := range ipList {
		// Check if it's a CIDR notation
		if strings.Contains(ipStr, "/") {
			_, ipNet, err := net.ParseCIDR(ipStr)
			if err != nil {
				logger.Error("Invalid CIDR notation",
					zap.String("cidr", ipStr),
					zap.Error(err))
				continue
			}
			nets = append(nets, ipNet)
		} else {
			// Single IP address
			ip := net.ParseIP(ipStr)
			if ip == nil {
				logger.Error("Invalid IP address",
					zap.String("ip", ipStr))
				continue
			}

			// Convert to CIDR (single IP = /32 for IPv4, /128 for IPv6)
			var ipNet *net.IPNet
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(ipStr + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(ipStr + "/128")
			}
			nets = append(nets, ipNet)
		}
	}

	return nets
}
