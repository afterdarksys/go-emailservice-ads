package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

// Metrics provides Prometheus-style metrics collection
// Using simple counters and gauges for now (can be extended with prometheus/client_golang)
type Metrics struct {
	logger *zap.Logger

	// Counters
	messagesReceived   int64
	messagesSent       int64
	messagesDelivered  int64
	messagesFailed     int64
	authSuccesses      int64
	authFailures       int64
	connectionsTotal   int64
	greylisted         int64
	spfPass            int64
	spfFail            int64
	dkimPass           int64
	dkimFail           int64

	// DANE metrics (RFC 7672)
	daneLookups        int64
	daneSuccesses      int64
	daneFailures       int64
	daneCacheHits      int64
	dnssecValidations  int64
	dnssecFailures     int64

	// Gauges
	queueDepth         int64
	activeConnections  int64
	daneEnabledDomains int64

	mu sync.RWMutex
}

// NewMetrics creates a new metrics collector
func NewMetrics(logger *zap.Logger) *Metrics {
	return &Metrics{
		logger: logger,
	}
}

// IncrementMessagesReceived increments the messages received counter
func (m *Metrics) IncrementMessagesReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesReceived++
}

// IncrementMessagesSent increments the messages sent counter
func (m *Metrics) IncrementMessagesSent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesSent++
}

// IncrementMessagesDelivered increments the messages delivered counter
func (m *Metrics) IncrementMessagesDelivered() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesDelivered++
}

// IncrementMessagesFailed increments the messages failed counter
func (m *Metrics) IncrementMessagesFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesFailed++
}

// IncrementAuthSuccesses increments the auth successes counter
func (m *Metrics) IncrementAuthSuccesses() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authSuccesses++
}

// IncrementAuthFailures increments the auth failures counter
func (m *Metrics) IncrementAuthFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authFailures++
}

// IncrementConnectionsTotal increments the total connections counter
func (m *Metrics) IncrementConnectionsTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionsTotal++
}

// IncrementGreylisted increments the greylisted counter
func (m *Metrics) IncrementGreylisted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.greylisted++
}

// IncrementSPFPass increments the SPF pass counter
func (m *Metrics) IncrementSPFPass() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spfPass++
}

// IncrementSPFFail increments the SPF fail counter
func (m *Metrics) IncrementSPFFail() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spfFail++
}

// IncrementDKIMPass increments the DKIM pass counter
func (m *Metrics) IncrementDKIMPass() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dkimPass++
}

// IncrementDKIMFail increments the DKIM fail counter
func (m *Metrics) IncrementDKIMFail() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dkimFail++
}

// IncrementDANELookups increments the DANE lookup counter
func (m *Metrics) IncrementDANELookups() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daneLookups++
}

// IncrementDANESuccesses increments the DANE success counter
func (m *Metrics) IncrementDANESuccesses() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daneSuccesses++
}

// IncrementDANEFailures increments the DANE failure counter
func (m *Metrics) IncrementDANEFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daneFailures++
}

// IncrementDANECacheHits increments the DANE cache hit counter
func (m *Metrics) IncrementDANECacheHits() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daneCacheHits++
}

// IncrementDNSSECValidations increments the DNSSEC validation counter
func (m *Metrics) IncrementDNSSECValidations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dnssecValidations++
}

// IncrementDNSSECFailures increments the DNSSEC failure counter
func (m *Metrics) IncrementDNSSECFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dnssecFailures++
}

// SetQueueDepth sets the current queue depth
func (m *Metrics) SetQueueDepth(depth int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueDepth = depth
}

// SetActiveConnections sets the current active connections count
func (m *Metrics) SetActiveConnections(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeConnections = count
}

// SetDANEEnabledDomains sets the count of domains with DANE enabled
func (m *Metrics) SetDANEEnabledDomains(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daneEnabledDomains = count
}

// GetSnapshot returns a snapshot of current metrics
func (m *Metrics) GetSnapshot() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]int64{
		"messages_received":   m.messagesReceived,
		"messages_sent":       m.messagesSent,
		"messages_delivered":  m.messagesDelivered,
		"messages_failed":     m.messagesFailed,
		"auth_successes":      m.authSuccesses,
		"auth_failures":       m.authFailures,
		"connections_total":   m.connectionsTotal,
		"greylisted":          m.greylisted,
		"spf_pass":            m.spfPass,
		"spf_fail":            m.spfFail,
		"dkim_pass":           m.dkimPass,
		"dkim_fail":           m.dkimFail,
		"dane_lookups":        m.daneLookups,
		"dane_successes":      m.daneSuccesses,
		"dane_failures":       m.daneFailures,
		"dane_cache_hits":     m.daneCacheHits,
		"dnssec_validations":  m.dnssecValidations,
		"dnssec_failures":     m.dnssecFailures,
		"queue_depth":         m.queueDepth,
		"active_connections":  m.activeConnections,
		"dane_enabled_domains": m.daneEnabledDomains,
	}
}

// Handler returns an HTTP handler for metrics endpoint
// Format: Prometheus text format
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		snapshot := m.GetSnapshot()

		// Write metrics in Prometheus format
		w.Write([]byte("# HELP messages_received Total messages received\n"))
		w.Write([]byte("# TYPE messages_received counter\n"))
		w.Write([]byte("messages_received " + format(snapshot["messages_received"]) + "\n\n"))

		w.Write([]byte("# HELP messages_sent Total messages sent\n"))
		w.Write([]byte("# TYPE messages_sent counter\n"))
		w.Write([]byte("messages_sent " + format(snapshot["messages_sent"]) + "\n\n"))

		w.Write([]byte("# HELP messages_delivered Total messages delivered\n"))
		w.Write([]byte("# TYPE messages_delivered counter\n"))
		w.Write([]byte("messages_delivered " + format(snapshot["messages_delivered"]) + "\n\n"))

		w.Write([]byte("# HELP messages_failed Total messages failed\n"))
		w.Write([]byte("# TYPE messages_failed counter\n"))
		w.Write([]byte("messages_failed " + format(snapshot["messages_failed"]) + "\n\n"))

		w.Write([]byte("# HELP auth_successes Total successful authentications\n"))
		w.Write([]byte("# TYPE auth_successes counter\n"))
		w.Write([]byte("auth_successes " + format(snapshot["auth_successes"]) + "\n\n"))

		w.Write([]byte("# HELP auth_failures Total failed authentications\n"))
		w.Write([]byte("# TYPE auth_failures counter\n"))
		w.Write([]byte("auth_failures " + format(snapshot["auth_failures"]) + "\n\n"))

		w.Write([]byte("# HELP connections_total Total connections\n"))
		w.Write([]byte("# TYPE connections_total counter\n"))
		w.Write([]byte("connections_total " + format(snapshot["connections_total"]) + "\n\n"))

		w.Write([]byte("# HELP greylisted Total messages greylisted\n"))
		w.Write([]byte("# TYPE greylisted counter\n"))
		w.Write([]byte("greylisted " + format(snapshot["greylisted"]) + "\n\n"))

		w.Write([]byte("# HELP spf_pass Total SPF passes\n"))
		w.Write([]byte("# TYPE spf_pass counter\n"))
		w.Write([]byte("spf_pass " + format(snapshot["spf_pass"]) + "\n\n"))

		w.Write([]byte("# HELP spf_fail Total SPF failures\n"))
		w.Write([]byte("# TYPE spf_fail counter\n"))
		w.Write([]byte("spf_fail " + format(snapshot["spf_fail"]) + "\n\n"))

		w.Write([]byte("# HELP dkim_pass Total DKIM passes\n"))
		w.Write([]byte("# TYPE dkim_pass counter\n"))
		w.Write([]byte("dkim_pass " + format(snapshot["dkim_pass"]) + "\n\n"))

		w.Write([]byte("# HELP dkim_fail Total DKIM failures\n"))
		w.Write([]byte("# TYPE dkim_fail counter\n"))
		w.Write([]byte("dkim_fail " + format(snapshot["dkim_fail"]) + "\n\n"))

		// DANE metrics
		w.Write([]byte("# HELP dane_lookups Total DANE TLSA lookups\n"))
		w.Write([]byte("# TYPE dane_lookups counter\n"))
		w.Write([]byte("dane_lookups " + format(snapshot["dane_lookups"]) + "\n\n"))

		w.Write([]byte("# HELP dane_successes Total successful DANE validations\n"))
		w.Write([]byte("# TYPE dane_successes counter\n"))
		w.Write([]byte("dane_successes " + format(snapshot["dane_successes"]) + "\n\n"))

		w.Write([]byte("# HELP dane_failures Total DANE validation failures\n"))
		w.Write([]byte("# TYPE dane_failures counter\n"))
		w.Write([]byte("dane_failures " + format(snapshot["dane_failures"]) + "\n\n"))

		w.Write([]byte("# HELP dane_cache_hits Total DANE cache hits\n"))
		w.Write([]byte("# TYPE dane_cache_hits counter\n"))
		w.Write([]byte("dane_cache_hits " + format(snapshot["dane_cache_hits"]) + "\n\n"))

		w.Write([]byte("# HELP dnssec_validations Total DNSSEC validations\n"))
		w.Write([]byte("# TYPE dnssec_validations counter\n"))
		w.Write([]byte("dnssec_validations " + format(snapshot["dnssec_validations"]) + "\n\n"))

		w.Write([]byte("# HELP dnssec_failures Total DNSSEC validation failures\n"))
		w.Write([]byte("# TYPE dnssec_failures counter\n"))
		w.Write([]byte("dnssec_failures " + format(snapshot["dnssec_failures"]) + "\n\n"))

		w.Write([]byte("# HELP queue_depth Current queue depth\n"))
		w.Write([]byte("# TYPE queue_depth gauge\n"))
		w.Write([]byte("queue_depth " + format(snapshot["queue_depth"]) + "\n\n"))

		w.Write([]byte("# HELP active_connections Current active connections\n"))
		w.Write([]byte("# TYPE active_connections gauge\n"))
		w.Write([]byte("active_connections " + format(snapshot["active_connections"]) + "\n\n"))

		w.Write([]byte("# HELP dane_enabled_domains Count of domains with DANE enabled\n"))
		w.Write([]byte("# TYPE dane_enabled_domains gauge\n"))
		w.Write([]byte("dane_enabled_domains " + format(snapshot["dane_enabled_domains"]) + "\n\n"))
	}
}

func format(n int64) string {
	return fmt.Sprintf("%d", n)
}
