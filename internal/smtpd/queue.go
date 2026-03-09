package smtpd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
	"github.com/afterdarksys/go-emailservice-ads/internal/elasticsearch"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// QueueTier represents the priority/type of the message queue
type QueueTier string

const (
	TierEmergency QueueTier = "emergency"
	TierMSA       QueueTier = "msa"  // Mail Submission Agent (Interactive user send)
	TierInt       QueueTier = "int"  // Internal routing
	TierOut       QueueTier = "out"  // Outbound routing
	TierBulk      QueueTier = "bulk" // Newsletters, notifications
)

// Message is a placeholder for the parsed email data and metadata
type Message struct {
	ID            string
	TraceID       string    // Global correlation ID for tracking across instances
	ParentTraceID string    // Parent trace ID for related messages (bounces, retries)
	InstanceID    string    // Pod/instance identifier for Kubernetes deployments
	From          string
	To            []string
	Data          []byte
	CreatedAt     time.Time
	Tier          QueueTier
	ContentHash   string    // SHA256 hash of message content
	ClientIP      string    // Client IP address
	HeloHostname  string    // HELO/EHLO hostname
}

// QueueManager handles the multi-tier queuing system
// Designed for high volume concurrency using buffered channels and worker pools.
type QueueManager struct {
	logger *zap.Logger
	store  *storage.MessageStore

	emergency chan *Message
	msa       chan *Message
	intQ      chan *Message
	out       chan *Message
	bulk      chan *Message

	// Rate limiters for each tier (messages per second)
	emergencyLimiter *rate.Limiter
	msaLimiter       *rate.Limiter
	intLimiter       *rate.Limiter
	outLimiter       *rate.Limiter
	bulkLimiter      *rate.Limiter

	// Delivery components
	mailDelivery    *delivery.MailDelivery
	bounceGenerator *bounce.BounceGenerator
	hostname        string
	localDomains    map[string]bool

	// Elasticsearch integration (optional)
	esIndexer  *elasticsearch.Indexer
	instanceID string // Instance/pod identifier

	// Metrics
	metrics *QueueMetrics
	metricsMu sync.RWMutex

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// QueueMetrics tracks queue performance
type QueueMetrics struct {
	Enqueued   map[QueueTier]int64
	Processed  map[QueueTier]int64
	Failed     map[QueueTier]int64
	Duplicates int64
	LastUpdate time.Time
}

// NewQueueManager initializes queue channels and starts workers
func NewQueueManager(logger *zap.Logger, store *storage.MessageStore, hostname string, localDomains []string) *QueueManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Create DNS resolver
	resolver := dns.NewResolver(logger)

	// Create mail delivery handler
	mailDelivery := delivery.NewMailDelivery(logger, resolver, hostname)

	// Create bounce generator
	bounceGen := bounce.NewBounceGenerator(hostname, fmt.Sprintf("postmaster@%s", hostname))

	// Build local domains map
	localDomainsMap := make(map[string]bool)
	for _, domain := range localDomains {
		localDomainsMap[strings.ToLower(domain)] = true
	}

	qm := &QueueManager{
		logger:    logger,
		store:     store,
		emergency: make(chan *Message, 10000), // Larger buffers for high throughput
		msa:       make(chan *Message, 50000),
		intQ:      make(chan *Message, 100000), // highest volume expected here for internal routing
		out:       make(chan *Message, 50000),
		bulk:      make(chan *Message, 100000),

		// Rate limiters: emergency=unlimited, msa=1000/s, int=5000/s, out=500/s, bulk=100/s
		emergencyLimiter: rate.NewLimiter(rate.Inf, 0),
		msaLimiter:       rate.NewLimiter(1000, 2000),
		intLimiter:       rate.NewLimiter(5000, 10000),
		outLimiter:       rate.NewLimiter(500, 1000),
		bulkLimiter:      rate.NewLimiter(100, 500),

		mailDelivery:    mailDelivery,
		bounceGenerator: bounceGen,
		hostname:        hostname,
		localDomains:    localDomainsMap,
		instanceID:      getInstanceID(),

		metrics: &QueueMetrics{
			Enqueued:  make(map[QueueTier]int64),
			Processed: make(map[QueueTier]int64),
			Failed:    make(map[QueueTier]int64),
		},

		ctx:    ctx,
		cancel: cancel,
	}

	qm.startWorkers()

	// Start DNS cache cleanup
	resolver.StartCacheCleanup(ctx)

	return qm
}

func (qm *QueueManager) startWorkers() {
	// Example worker pool sizing - in a real app this should be configurable
	qm.spawnWorkers("emergency", qm.emergency, 50)
	qm.spawnWorkers("msa", qm.msa, 200)
	qm.spawnWorkers("int", qm.intQ, 500) // very high concurrency for internal routing
	qm.spawnWorkers("out", qm.out, 200)
	qm.spawnWorkers("bulk", qm.bulk, 100)
}

func (qm *QueueManager) spawnWorkers(name string, ch <-chan *Message, count int) {
	for i := 0; i < count; i++ {
		qm.wg.Add(1)
		workerID := i
		go func() {
			defer qm.wg.Done()
			for {
				select {
				case <-qm.ctx.Done():
					qm.logger.Debug("Worker stopped", zap.String("queue", name), zap.Int("id", workerID))
					return
				case msg := <-ch:
					qm.processMessage(name, msg)
				}
			}
		}()
	}
	qm.logger.Info("Started workers for queue category", zap.String("queue", name), zap.Int("workers", count))
}

func (qm *QueueManager) processMessage(queueName string, msg *Message) {
	// Apply rate limiting
	limiter := qm.getLimiter(msg.Tier)
	if err := limiter.Wait(qm.ctx); err != nil {
		qm.logger.Error("Rate limiter error", zap.Error(err))
		return
	}

	qm.logger.Debug("Processing message",
		zap.String("queue", queueName),
		zap.String("msg_id", msg.ID),
		zap.String("from", msg.From),
		zap.Int("recipients", len(msg.To)))

	// Publish processing event to Elasticsearch
	qm.publishEvent(elasticsearch.EventProcessing, msg, nil)

	// Separate local and remote recipients
	localRecipients := make([]string, 0)
	remoteRecipients := make([]string, 0)

	for _, rcpt := range msg.To {
		domain := qm.extractDomain(rcpt)
		if qm.localDomains[strings.ToLower(domain)] {
			localRecipients = append(localRecipients, rcpt)
		} else {
			remoteRecipients = append(remoteRecipients, rcpt)
		}
	}

	// Process local delivery (to IMAP/Maildir)
	if len(localRecipients) > 0 {
		qm.deliverLocal(msg, localRecipients)
	}

	// Process remote delivery (via SMTP)
	if len(remoteRecipients) > 0 {
		qm.deliverRemote(msg, remoteRecipients)
	}

	// Update message status in store
	if err := qm.store.UpdateStatus(msg.ID, "delivered", ""); err != nil {
		qm.logger.Error("Failed to update message status", zap.String("msg_id", msg.ID), zap.Error(err))
		qm.updateMetrics(msg.Tier, "failed")
		qm.store.UpdateStatus(msg.ID, "pending", err.Error())
		return
	}

	qm.updateMetrics(msg.Tier, "processed")
}

// deliverLocal handles local message delivery
func (qm *QueueManager) deliverLocal(msg *Message, recipients []string) {
	// TODO: Integrate with IMAP storage or Maildir
	// For now, just log
	qm.logger.Info("Local delivery",
		zap.String("msg_id", msg.ID),
		zap.Int("recipients", len(recipients)))
}

// deliverRemote handles remote SMTP delivery
func (qm *QueueManager) deliverRemote(msg *Message, recipients []string) {
	ctx, cancel := context.WithTimeout(qm.ctx, 5*time.Minute)
	defer cancel()

	startTime := time.Now()
	result, err := qm.mailDelivery.Deliver(ctx, msg.From, recipients, msg.Data)
	latencyMs := time.Since(startTime).Milliseconds()

	if err != nil {
		qm.logger.Error("Remote delivery failed",
			zap.String("msg_id", msg.ID),
			zap.Error(err))

		// Publish failure event to Elasticsearch
		extra := map[string]interface{}{
			"delivery": elasticsearch.DeliveryInfo{
				RemoteHost:    getRemoteHost(result),
				RemoteIP:      "", // Not available in DeliveryResult
				SMTPCode:      getSMTPCode(result),
				LatencyMs:     latencyMs,
				AttemptNumber: getAttemptNumber(msg),
				IsPermanent:   result != nil && result.IsPermanent,
			},
			"error": elasticsearch.ErrorInfo{
				Message:   getErrorMessage(err, result),
				Category:  "delivery",
				Retryable: result == nil || !result.IsPermanent,
			},
		}
		qm.publishEvent(elasticsearch.EventFailed, msg, extra)

		// Generate bounce message if permanent failure
		if result != nil && result.IsPermanent {
			qm.generateBounce(msg, result, recipients)
		}

		// Update status for retry
		errorMsg := "delivery failed"
		if result != nil {
			errorMsg = result.Message
		}
		qm.store.UpdateStatus(msg.ID, "pending", errorMsg)
		return
	}

	// Publish success event to Elasticsearch
	extra := map[string]interface{}{
		"delivery": elasticsearch.DeliveryInfo{
			RemoteHost:    result.RemoteHost,
			RemoteIP:      "", // Not available in DeliveryResult
			SMTPCode:      result.SMTPCode,
			LatencyMs:     latencyMs,
			AttemptNumber: getAttemptNumber(msg),
			IsPermanent:   false,
		},
	}
	qm.publishEvent(elasticsearch.EventDelivered, msg, extra)

	qm.logger.Info("Remote delivery successful",
		zap.String("msg_id", msg.ID),
		zap.String("remote_host", result.RemoteHost),
		zap.Int("recipients", len(recipients)))
}

// generateBounce creates and sends a bounce message
func (qm *QueueManager) generateBounce(msg *Message, result *delivery.DeliveryResult, recipients []string) {
	for _, rcpt := range recipients {
		reason := &bounce.BounceReason{
			SMTPCode:     result.SMTPCode,
			EnhancedCode: bounce.GetEnhancedStatusCode(result.SMTPCode, result.Message),
			Message:      result.Message,
			IsPermanent:  result.IsPermanent,
			RemoteHost:   result.RemoteHost,
			Recipient:    rcpt,
		}

		bounceMsg, err := qm.bounceGenerator.GenerateBounce(msg.From, reason, msg.Data)
		if err != nil {
			qm.logger.Error("Failed to generate bounce",
				zap.String("msg_id", msg.ID),
				zap.String("recipient", rcpt),
				zap.Error(err))
			continue
		}

		// Enqueue bounce message (send to original sender)
		bounceEnvelope := &Message{
			From:          fmt.Sprintf("postmaster@%s", qm.hostname),
			To:            []string{msg.From},
			Data:          bounceMsg,
			CreatedAt:     time.Now(),
			Tier:          TierEmergency, // High priority for bounces
			ParentTraceID: msg.TraceID,   // Link to original message
		}

		if err := qm.Enqueue(bounceEnvelope); err != nil {
			qm.logger.Error("Failed to enqueue bounce",
				zap.String("msg_id", msg.ID),
				zap.Error(err))
		} else {
			qm.logger.Info("Bounce message generated",
				zap.String("original_msg_id", msg.ID),
				zap.String("recipient", rcpt))

			// Publish bounce event to Elasticsearch
			extra := map[string]interface{}{
				"parent_trace_id": msg.TraceID,
				"delivery": elasticsearch.DeliveryInfo{
					RemoteHost:  result.RemoteHost,
					RemoteIP:    "", // Not available in DeliveryResult
					SMTPCode:    result.SMTPCode,
					IsPermanent: result.IsPermanent,
				},
			}
			qm.publishEvent(elasticsearch.EventBounce, msg, extra)
		}
	}
}

// extractDomain extracts the domain from an email address
func (qm *QueueManager) extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func (qm *QueueManager) getLimiter(tier QueueTier) *rate.Limiter {
	switch tier {
	case TierEmergency:
		return qm.emergencyLimiter
	case TierMSA:
		return qm.msaLimiter
	case TierInt:
		return qm.intLimiter
	case TierOut:
		return qm.outLimiter
	case TierBulk:
		return qm.bulkLimiter
	default:
		return qm.outLimiter
	}
}

func (qm *QueueManager) updateMetrics(tier QueueTier, metricType string) {
	qm.metricsMu.Lock()
	defer qm.metricsMu.Unlock()

	switch metricType {
	case "enqueued":
		qm.metrics.Enqueued[tier]++
	case "processed":
		qm.metrics.Processed[tier]++
	case "failed":
		qm.metrics.Failed[tier]++
	case "duplicate":
		qm.metrics.Duplicates++
	}
	qm.metrics.LastUpdate = time.Now()
}

// Enqueue submits a message to the appropriate tier without blocking the SMTP session
func (qm *QueueManager) Enqueue(msg *Message) error {
	// Generate trace ID if not set
	if msg.TraceID == "" {
		msg.TraceID = generateTraceID()
	}

	// Compute content hash if not set
	if msg.ContentHash == "" {
		msg.ContentHash = qm.computeContentHash(msg.Data)
	}

	// Store message persistently first (disaster recovery)
	entry := &storage.JournalEntry{
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Data:      msg.Data,
		Tier:      string(msg.Tier),
	}

	messageID, isDuplicate, err := qm.store.Store(entry)
	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	if isDuplicate {
		qm.updateMetrics(msg.Tier, "duplicate")
		qm.logger.Info("Duplicate message rejected",
			zap.String("msg_id", messageID),
			zap.String("from", msg.From))
		return nil
	}

	msg.ID = messageID
	qm.updateMetrics(msg.Tier, "enqueued")

	// Publish enqueued event to Elasticsearch
	qm.publishEvent(elasticsearch.EventEnqueued, msg, nil)

	// Enqueue to in-memory channel for processing
	switch msg.Tier {
	case TierEmergency:
		qm.emergency <- msg
	case TierMSA:
		qm.msa <- msg
	case TierInt:
		qm.intQ <- msg
	case TierOut:
		qm.out <- msg
	case TierBulk:
		qm.bulk <- msg
	default:
		// Fallback
		qm.logger.Warn("Unknown tier, falling back to out queue", zap.String("tier", string(msg.Tier)))
		qm.out <- msg
	}

	return nil
}

// GetMetrics returns current queue metrics
func (qm *QueueManager) GetMetrics() *QueueMetrics {
	qm.metricsMu.RLock()
	defer qm.metricsMu.RUnlock()

	// Create a copy to avoid race conditions
	metrics := &QueueMetrics{
		Enqueued:   make(map[QueueTier]int64),
		Processed:  make(map[QueueTier]int64),
		Failed:     make(map[QueueTier]int64),
		Duplicates: qm.metrics.Duplicates,
		LastUpdate: qm.metrics.LastUpdate,
	}

	for k, v := range qm.metrics.Enqueued {
		metrics.Enqueued[k] = v
	}
	for k, v := range qm.metrics.Processed {
		metrics.Processed[k] = v
	}
	for k, v := range qm.metrics.Failed {
		metrics.Failed[k] = v
	}

	return metrics
}

// SetElasticsearchIndexer sets the Elasticsearch indexer for event publishing
func (qm *QueueManager) SetElasticsearchIndexer(indexer *elasticsearch.Indexer) {
	qm.esIndexer = indexer
	qm.logger.Info("Elasticsearch indexer attached to queue manager",
		zap.String("instance_id", qm.instanceID))
}

// publishEvent publishes an event to Elasticsearch if indexer is configured
func (qm *QueueManager) publishEvent(eventType elasticsearch.EventType, msg *Message, extra map[string]interface{}) {
	if qm.esIndexer == nil {
		return
	}

	// Build event
	event := elasticsearch.BuildEvent(
		eventType,
		msg.ID,
		msg.TraceID,
		qm.instanceID,
		msg.From,
		msg.To,
		string(msg.Tier),
	)

	// Set envelope
	event.SetEnvelope(msg.From, msg.To, len(msg.Data))

	// Set metadata
	event.SetMetadata(msg.ContentHash, msg.ClientIP, "", msg.HeloHostname)

	// Add any extra fields from the map
	if extra != nil {
		if security, ok := extra["security"].(elasticsearch.SecurityInfo); ok {
			event.Security = security
		}
		if delivery, ok := extra["delivery"].(elasticsearch.DeliveryInfo); ok {
			event.Delivery = delivery
		}
		if policy, ok := extra["policy"].(elasticsearch.PolicyInfo); ok {
			event.Policy = policy
		}
		if errInfo, ok := extra["error"].(elasticsearch.ErrorInfo); ok {
			event.Error = errInfo
		}
		if parentTraceID, ok := extra["parent_trace_id"].(string); ok {
			event.SetParentTrace(parentTraceID)
		}
	}

	// Publish event (non-blocking)
	qm.esIndexer.PublishEvent(event)
}

// computeContentHash computes SHA256 hash of message data
func (qm *QueueManager) computeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Shutdown gracefully stops the queue manager, ensuring workers drain or stop
func (qm *QueueManager) Shutdown() {
	qm.logger.Info("Shutting down QueueManager...")
	qm.cancel()
	qm.wg.Wait()

	// Shutdown mail delivery (close connection pools)
	if err := qm.mailDelivery.Shutdown(); err != nil {
		qm.logger.Error("Error shutting down mail delivery", zap.Error(err))
	}

	// Shutdown Elasticsearch indexer if configured
	if qm.esIndexer != nil {
		qm.logger.Info("Closing Elasticsearch indexer...")
		if err := qm.esIndexer.Close(); err != nil {
			qm.logger.Error("Error closing Elasticsearch indexer", zap.Error(err))
		}
	}

	qm.logger.Info("QueueManager stopped gracefully")
}

// generateTraceID generates a unique trace ID for message correlation
func generateTraceID() string {
	// Use timestamp + random UUID for trace ID
	// Format: trace_<timestamp>_<uuid>
	return fmt.Sprintf("trace_%d_%s", time.Now().UnixNano(), generateShortID())
}

// generateShortID generates a short random ID
func generateShortID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(1 * time.Nanosecond) // Ensure uniqueness
	}
	return string(b)
}

// getInstanceID returns the instance/pod identifier
func getInstanceID() string {
	// Try to get from environment (Kubernetes sets this)
	if hostname := os.Getenv("HOSTNAME"); hostname != "" {
		return hostname
	}

	// Try to get from POD_NAME env var
	if podName := os.Getenv("POD_NAME"); podName != "" {
		return podName
	}

	// Fall back to system hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// Helper functions for delivery result extraction

func getRemoteHost(result *delivery.DeliveryResult) string {
	if result != nil {
		return result.RemoteHost
	}
	return ""
}

func getSMTPCode(result *delivery.DeliveryResult) int {
	if result != nil {
		return result.SMTPCode
	}
	return 0
}

func getAttemptNumber(msg *Message) int {
	// This should be tracked in the Message struct or retrieved from storage
	// For now, return 1 as default
	return 1
}

func getErrorMessage(err error, result *delivery.DeliveryResult) string {
	if result != nil && result.Message != "" {
		return result.Message
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}
