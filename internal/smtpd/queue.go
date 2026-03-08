package smtpd

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
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
	ID        string
	From      string
	To        []string
	Data      []byte
	CreatedAt time.Time
	Tier      QueueTier
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

	result, err := qm.mailDelivery.Deliver(ctx, msg.From, recipients, msg.Data)

	if err != nil {
		qm.logger.Error("Remote delivery failed",
			zap.String("msg_id", msg.ID),
			zap.Error(err))

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
			From:      fmt.Sprintf("postmaster@%s", qm.hostname),
			To:        []string{msg.From},
			Data:      bounceMsg,
			CreatedAt: time.Now(),
			Tier:      TierEmergency, // High priority for bounces
		}

		if err := qm.Enqueue(bounceEnvelope); err != nil {
			qm.logger.Error("Failed to enqueue bounce",
				zap.String("msg_id", msg.ID),
				zap.Error(err))
		} else {
			qm.logger.Info("Bounce message generated",
				zap.String("original_msg_id", msg.ID),
				zap.String("recipient", rcpt))
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

// Shutdown gracefully stops the queue manager, ensuring workers drain or stop
func (qm *QueueManager) Shutdown() {
	qm.logger.Info("Shutting down QueueManager...")
	qm.cancel()
	qm.wg.Wait()

	// Shutdown mail delivery (close connection pools)
	if err := qm.mailDelivery.Shutdown(); err != nil {
		qm.logger.Error("Error shutting down mail delivery", zap.Error(err))
	}

	qm.logger.Info("QueueManager stopped gracefully")
}
