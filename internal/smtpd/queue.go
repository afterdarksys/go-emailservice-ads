package smtpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstorm"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"github.com/emersion/go-msgauth/dkim"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/google/uuid"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/delivery"
	"github.com/afterdarksys/go-emailservice-ads/internal/dns"
	"github.com/afterdarksys/go-emailservice-ads/internal/elasticsearch"
	"github.com/afterdarksys/go-emailservice-ads/internal/filtering"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"github.com/afterdarksys/go-emailservice-ads/internal/security/dane"
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
	AdmissionKey     string `json:"admission_key,omitempty"`
	DeliveryFolder   string
	ID               string
	TraceID          string // Global correlation ID for tracking across instances
	ParentTraceID    string // Parent trace ID for related messages (bounces, retries)
	InstanceID       string // Pod/instance identifier for Kubernetes deployments
	From             string
	To               []string
	Data             []byte
	CreatedAt        time.Time
	Tier             QueueTier
	ContentHash      string            // SHA256 hash of message content
	ClientIP         string            // Client IP address
	HeloHostname     string            // HELO/EHLO hostname
	DKIMResult       string            // Result of DKIM verification ("pass", "fail", "none")
	SPFResult        string            // Result of SPF verification ("pass", "fail", "softfail", "none", ...)
	DMARCResult      string            // Result of DMARC evaluation ("pass", "fail", "none")
	ExtraHeaders     map[string]string // Additional headers to prepend to message
	IsBounce         bool              // True if this message is a bounce/DSN
	Quarantine       bool              // True if DMARC enforce mode quarantined this message
	QuarantineFolder string            // Target folder for quarantined local delivery (e.g. "Junk")
}

// QueueManager handles the multi-tier queuing system
// Designed for high volume concurrency using buffered channels and worker pools.
type QueueManager struct {
	reputationDB *filtering.PremailReputation
	platform     config.PlatformConfig
	abuse        abuseLimits
	StormGuard   *mailstorm.Guard
	users        *auth.UserStore
	dataDir      string
	logger       *zap.Logger
	store        *storage.MessageStore
	imapStore    *storage.MailboxStore

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

	// dkimSigners signs outbound mail, keyed by lowercase sending domain. A
	// missing key means that domain isn't signed.
	dkimSigners map[string]*security.Signer

	// Elasticsearch integration (optional)
	esIndexer  *elasticsearch.Indexer
	instanceID string // Instance/pod identifier

	// Policy manager for per-user Sieve filtering (optional)
	policyManager *policy.Manager

	// Metrics
	metrics   *QueueMetrics
	metricsMu sync.RWMutex

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// QueueMetrics tracks queue performance
type QueueMetrics struct {
	Enqueued     map[QueueTier]int64
	Processed    map[QueueTier]int64
	Failed       map[QueueTier]int64
	Duplicates   int64
	Backpressure int64
	LastUpdate   time.Time
}

// NewQueueManager initializes queue channels and starts workers. dkimSigners
// maps lowercase sending domain to its signer; a nil/empty map disables
// outbound DKIM signing entirely.
func NewQueueManager(logger *zap.Logger, store *storage.MessageStore, imapStore *storage.MailboxStore, hostname string, localDomains []string, dkimSigners map[string]*security.Signer) *QueueManager {
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
		imapStore: imapStore,
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
		dkimSigners:     dkimSigners,
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
	if qm.StormGuard != nil && qm.StormGuard.Blocked(msg.AdmissionKey) {
		if _, err := qm.store.Transition(msg.ID, "queued", "pending"); err != nil {
			qm.logger.Error("Unable to defer paused sender", zap.Error(err))
		}
		return
	}

	// Apply rate limiting
	limiter := qm.getLimiter(msg.Tier)
	if err := limiter.Wait(qm.ctx); err != nil {
		qm.logger.Error("Rate limiter error", zap.Error(err))
		qm.store.Transition(msg.ID, "queued", "pending")
		return
	}

	qm.logger.Debug("Processing message",
		zap.String("queue", queueName),
		zap.String("msg_id", msg.ID),
		zap.String("from", msg.From),
		zap.Int("recipients", len(msg.To)))

	// Publish processing event to Elasticsearch
	qm.publishEvent(elasticsearch.EventProcessing, msg, nil)

	var deliveryErr error
	var remaining []string
	for i, rcpt := range msg.To {
		if qm.StormGuard != nil && qm.StormGuard.Blocked(msg.AdmissionKey) {
			if i == 0 {
				qm.store.Transition(msg.ID, "queued", "pending")
				return
			}
			remaining = append(remaining, msg.To[i:]...)
			deliveryErr = errors.Join(deliveryErr, fmt.Errorf("sender paused by mailstorm protection"))
			break
		}
		if i == 0 {
			if ok, err := qm.store.Transition(msg.ID, "queued", "processing"); err != nil || !ok {
				return
			}
		}
		attempt := *msg
		attempt.To = []string{rcpt}
		domain := qm.extractDomain(rcpt)
		var err error
		if qm.localDomains[strings.ToLower(domain)] && !qm.mailDelivery.HasExplicitRoute(domain) {
			err = qm.deliverLocal(&attempt, []string{rcpt})
		} else {
			err = qm.deliverRemote(&attempt, []string{rcpt})
		}
		if err != nil {
			remaining = append(remaining, rcpt)
			deliveryErr = errors.Join(deliveryErr, err)
		}
		checkpoint := append(append([]string(nil), remaining...), msg.To[i+1:]...)
		if err := qm.store.UpdateRecipients(msg.ID, checkpoint); err != nil {
			qm.finalizeDelivery(msg, err)
			return
		}
	}
	if err := qm.store.UpdateRecipients(msg.ID, remaining); err != nil {
		qm.finalizeDelivery(msg, err)
		return
	}
	msg.To = remaining

	qm.finalizeDelivery(msg, deliveryErr)
}

// finalizeDelivery commits queue state only after every attempted delivery path
// has reported its result. A transient failure must remain durable for retry;
// it must never be overwritten by a successful sibling path.
func (qm *QueueManager) finalizeDelivery(msg *Message, deliveryErr error) {
	if deliveryErr != nil {
		qm.logger.Error("Message delivery incomplete; retaining for retry",
			zap.String("msg_id", msg.ID),
			zap.Error(deliveryErr))
		if err := qm.store.UpdateStatus(msg.ID, "pending", deliveryErr.Error()); err != nil {
			qm.logger.Error("Failed to retain message for retry", zap.String("msg_id", msg.ID), zap.Error(err))
		}
		qm.updateMetrics(msg.Tier, "failed")
		return
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

// deliverLocal handles local message delivery via IMAP store.
func (qm *QueueManager) deliverLocal(msg *Message, recipients []string) error {
	var deliveryErr error
	for _, rcpt := range recipients {
		// The full address, not just the local part, is the mailbox key —
		// it must match what IMAP Login uses (internal/imap/backend.go
		// passes the client's username straight through, and every
		// server.local_domains account here is provisioned as a full
		// address). Stripping to local-part would collide same-named
		// mailboxes across domains on a shared mailhub (help@a.com and
		// help@b.com both landing in "help") and never match how the
		// owner actually logs in to read it.
		username := rcpt
		if qm.users != nil {
			u, ok := qm.users.Recipient(rcpt)
			if !ok {
				deliveryErr = errors.Join(deliveryErr, fmt.Errorf("recipient unavailable: %s", rcpt))
				continue
			}
			username = u.Username
		}
		folder := msg.DeliveryFolder
		if folder == "" {
			folder = "INBOX"
		}

		// Run per-user Sieve script if policy manager is configured.
		if qm.policyManager != nil {
			scriptPath := filepath.Join(qm.dataDir, "sieve", username+".sieve")
			if scriptData, err := os.ReadFile(scriptPath); err == nil {
				emailCtx, ctxErr := policy.NewEmailContext(msg.From, msg.To, msg.ClientIP, msg.HeloHostname, msg.Data)
				if ctxErr != nil {
					qm.logger.Warn("Failed to build email context for Sieve, delivering to INBOX",
						zap.String("recipient", rcpt),
						zap.Error(ctxErr))
				} else {
					action, evalErr := qm.policyManager.EvaluateSieve(qm.ctx, string(scriptData), emailCtx)
					if evalErr != nil {
						qm.logger.Warn("Sieve evaluation failed, delivering to INBOX",
							zap.String("recipient", rcpt),
							zap.Error(evalErr))
					} else {
						switch action.Type {
						case policy.ActionFileinto:
							folder = action.Target
						case policy.ActionDiscard:
							qm.logger.Info("Sieve discarded message",
								zap.String("msg_id", msg.ID),
								zap.String("recipient", rcpt))
							continue
						case policy.ActionReject:
							qm.logger.Info("Sieve rejected message at delivery",
								zap.String("msg_id", msg.ID),
								zap.String("recipient", rcpt),
								zap.String("reason", action.Reason))
							continue
						}
					}
				}
			}
		}

		// DMARC quarantine (enforce mode): keep a failing message out of the inbox,
		// unless the user's Sieve script already routed it somewhere specific.
		if msg.Quarantine && folder == "INBOX" {
			folder = msg.QuarantineFolder
			if folder == "" {
				folder = "Junk"
			}
			qm.logger.Info("Delivering DMARC-quarantined message to quarantine folder",
				zap.String("msg_id", msg.ID),
				zap.String("recipient", rcpt),
				zap.String("folder", folder))
		}

		msgID, err := qm.imapStore.DeliverOnce(qm.ctx, msg.ID+"/"+rcpt, username, folder, msg.Data)
		if err != nil {
			qm.logger.Error("Local delivery failed",
				zap.String("recipient", rcpt),
				zap.Error(err))
			deliveryErr = errors.Join(deliveryErr, fmt.Errorf("local delivery to %s: %w", rcpt, err))
			continue
		}
		qm.logger.Info("Local delivery succeeded",
			zap.String("msg_id", msg.ID),
			zap.String("imap_id", msgID),
			zap.String("recipient", rcpt),
			zap.String("folder", folder))
		qm.publishEvent(elasticsearch.EventDelivered, msg, nil)
	}
	return deliveryErr
}

// signOutbound returns a DKIM-signed copy of msg.Data when a signing key is
// configured for the message's envelope-from domain. It returns (nil, nil)
// when signing does not apply here (no signer configured for this domain —
// this server must never sign mail on behalf of a domain it doesn't control,
// e.g. relayed/forwarded mail, or a domain sharing this mailhub that hasn't
// had a key provisioned), and (nil, err) when signing was expected to happen
// but failed. Callers must treat a non-nil error as a delivery failure rather
// than falling back to sending unsigned: shipping "From: user@gomeow.media"
// unsigned would misrepresent the domain's authentication to the recipient.
func (qm *QueueManager) signOutbound(msg *Message) ([]byte, error) {
	signer := qm.dkimSigners[strings.ToLower(qm.extractDomain(msg.From))]
	if signer == nil {
		return nil, nil
	}
	opts := signer.GetOptions()
	if opts == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := dkim.Sign(&buf, bytes.NewReader(msg.Data), opts); err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}
	return buf.Bytes(), nil
}

// deliverRemote handles remote SMTP delivery
func (qm *QueueManager) deliverRemote(msg *Message, recipients []string) error {
	ctx, cancel := context.WithTimeout(qm.ctx, 5*time.Minute)
	defer cancel()

	data, err := qm.signOutbound(msg)
	if err != nil {
		qm.logger.Error("DKIM signing failed; withholding unsigned mail for retry",
			zap.String("msg_id", msg.ID), zap.Error(err))
		return fmt.Errorf("dkim signing: %w", err)
	}
	if data == nil {
		data = msg.Data
	}

	startTime := time.Now()
	if len(recipients) > 0 && qm.mailDelivery.HasRoute(qm.extractDomain(recipients[0])) && net.ParseIP(msg.ClientIP) != nil {
		data = removeHeader(data, "X-Mailhub-Original-IP")
		data = append([]byte("X-Mailhub-Original-IP: "+msg.ClientIP+"\r\n"), data...)
	}
	result, err := qm.mailDelivery.Deliver(ctx, msg.From, recipients, data)
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

		if result != nil {
			if e := qm.handleRecipientOutcomes(msg, result, recipients); e != nil {
				return e
			}
			if !resultHasTemporaryRecipient(result) {
				return nil
			}
		}

		return fmt.Errorf("remote delivery: %w", err)
	}
	if result != nil {
		if e := qm.handleRecipientOutcomes(msg, result, recipients); e != nil {
			return e
		}
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
	return nil
}

func resultHasTemporaryRecipient(result *delivery.DeliveryResult) bool {
	for _, outcome := range result.Recipients {
		if !outcome.Success && !outcome.IsPermanent {
			return true
		}
	}
	return false
}

// handleRecipientOutcomes removes successful and permanent recipients from a
// durable retry transaction. Permanent recipients generate a DSN; only
// temporary recipients remain in the journal for the retry scheduler.
func (qm *QueueManager) handleRecipientOutcomes(msg *Message, result *delivery.DeliveryResult, fallback []string) error {
	if len(result.Recipients) == 0 {
		return nil
	}
	var retry, permanent []string
	for _, outcome := range result.Recipients {
		if outcome.Success {
			continue
		}
		if outcome.IsPermanent {
			permanent = append(permanent, outcome.Recipient)
		} else {
			retry = append(retry, outcome.Recipient)
		}
	}
	if len(permanent) > 0 {
		if err := qm.generateBounce(msg, result, permanent); err != nil {
			return err
		}
	}
	if len(retry) == 0 && len(permanent) > 0 {
		retry = nil
	}
	if len(retry) == 0 && len(permanent) == 0 {
		return nil
	}
	msg.To = retry
	return nil
}

// generateBounce creates and sends a bounce message
func (qm *QueueManager) generateBounce(msg *Message, result *delivery.DeliveryResult, recipients []string) error {
	// Never bounce to a null envelope sender — prevents bounce loops (RFC 5321 §4.5.5)
	if msg.From == "" || msg.From == "<>" {
		qm.logger.Info("Skipping bounce for null envelope sender", zap.String("msg_id", msg.ID))
		return nil
	}
	// Suppress double-bounce: do not bounce a bounce/DSN
	if msg.IsBounce {
		qm.logger.Warn("Suppressing double-bounce", zap.String("msg_id", msg.ID))
		return nil
	}

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
			return err
		}

		// Enqueue bounce message (send to original sender)
		bounceEnvelope := &Message{
			From:          "", // RFC 5321: DSN messages use null reverse-path
			To:            []string{msg.From},
			Data:          bounceMsg,
			CreatedAt:     time.Now(),
			Tier:          TierEmergency, // High priority for bounces
			ParentTraceID: msg.TraceID,   // Link to original message
			IsBounce:      true,
		}

		if err := qm.Enqueue(bounceEnvelope); err != nil {
			qm.logger.Error("Failed to enqueue bounce",
				zap.String("msg_id", msg.ID),
				zap.Error(err))
			return err
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
	return nil
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
	case "backpressure":
		qm.metrics.Backpressure++
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

	// Persist security context alongside the envelope for recovery.
	metadata := *msg
	metadata.Data = nil
	metadata.To = nil
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	// Store message persistently first (disaster recovery)
	entry := &storage.JournalEntry{
		MessageID: msg.ID,
		From:      msg.From,
		To:        msg.To,
		Data:      msg.Data,
		Tier:      string(msg.Tier),
		Metadata:  map[string]string{"message": string(encoded), "client_ip": msg.ClientIP},
	}

	if msg.Quarantine {
		entry.Status = "held"
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

	if msg.Quarantine {
		return nil
	}
	if ok, err := qm.store.Transition(msg.ID, "pending", "queued"); err != nil || !ok {
		return nil
	}
	// Enqueue to in-memory channel for processing (non-blocking with 100ms backpressure)
	return qm.enqueueToChannel(msg.Tier, msg)
}

// RequeueStored puts an already durable delivery transaction back on its
// in-memory queue. Retry paths must use this instead of Enqueue so they do not
// attempt a second persistence write for the same message ID.
func (qm *QueueManager) RequeueStored(msg *Message) error {
	return qm.enqueueToChannel(msg.Tier, msg)
}

// enqueueToChannel sends msg to the appropriate tier channel with a 100ms backpressure timeout.
// The message is already persisted to the store, so it is safe to return without blocking
// the SMTP session — the retry scheduler will reprocess it from the store.
func (qm *QueueManager) enqueueToChannel(tier QueueTier, msg *Message) error {
	ch := qm.channelForTier(tier)
	select {
	case ch <- msg:
		return nil
	case <-time.After(100 * time.Millisecond):
		qm.logger.Warn("Queue channel full — message will be retried from store",
			zap.String("tier", string(tier)),
			zap.String("msg_id", msg.ID))
		qm.updateMetrics(tier, "backpressure")
		if err := qm.store.UpdateStatus(msg.ID, "pending", "queue channel full"); err != nil {
			qm.logger.Error("Failed to retain backpressured message", zap.String("msg_id", msg.ID), zap.Error(err))
		}
		return nil
	case <-qm.ctx.Done():
		qm.store.UpdateStatus(msg.ID, "pending", "queue shutting down")
		return nil
	}
}

// channelForTier returns the channel for a given tier.
func (qm *QueueManager) channelForTier(tier QueueTier) chan *Message {
	switch tier {
	case TierEmergency:
		return qm.emergency
	case TierMSA:
		return qm.msa
	case TierInt:
		return qm.intQ
	case TierOut:
		return qm.out
	case TierBulk:
		return qm.bulk
	default:
		qm.logger.Warn("Unknown tier, falling back to out queue", zap.String("tier", string(tier)))
		return qm.out
	}
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

// SetPolicyManager attaches a policy manager for per-user Sieve filtering at delivery time.
func (qm *QueueManager) SetPolicyManager(pm *policy.Manager) {
	qm.policyManager = pm
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
	if qm.reputationDB != nil {
		qm.reputationDB.Close()
	}

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

// generateTraceID generates a unique trace ID for message correlation.
func generateTraceID() string {
	return "trace_" + uuid.New().String()
}

// generateShortID generates a cryptographically random UUID v4.
func generateShortID() string {
	return uuid.New().String()
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

func (qm *QueueManager) ConfigurePlatform(cfg *config.Config, users *auth.UserStore) error {
	if cfg.Platform.ReputationDatabaseEnv != "" {
		dsn := os.Getenv(cfg.Platform.ReputationDatabaseEnv)
		if dsn == "" {
			return fmt.Errorf("missing reputation database environment variable")
		}
		ctx, cancel := context.WithTimeout(qm.ctx, 5*time.Second)
		db, err := filtering.NewPremailReputation(ctx, dsn)
		cancel()
		if err != nil {
			return err
		}
		qm.reputationDB = db
	}
	guard, err := mailstorm.New(cfg.Platform.Mailstorm, filepath.Join(cfg.Platform.DataDir, "mailstorm.json"))
	if err != nil {
		return err
	}
	qm.StormGuard = guard
	qm.platform = cfg.Platform
	qm.abuse.buckets = make(map[string]*quotaBucket)
	qm.users = users
	qm.dataDir = cfg.Platform.DataDir
	qm.mailDelivery.SetRoutes(cfg.Platform.Transports)
	if cfg.Server.DANE.Enabled {
		qm.mailDelivery.SetDANEValidator(dane.NewDANEValidator(qm.logger, cfg.Server.DANE.DNSServers, cfg.Server.DANE.StrictMode))
	}
	qm.imapStore.SetQuota(cfg.Platform.MailboxQuotaBytes)
	if cfg.Platform.TLSReporting {
		reports, err := security.NewDurableTLSReports(filepath.Join(cfg.Platform.DataDir, "tls-reports"), qm.hostname)
		if err != nil {
			return err
		}
		reports.SendMail = qm.enqueueTLSReport
		qm.mailDelivery.SetTLSReports(reports)
		qm.wg.Add(1)
		go func() {
			defer qm.wg.Done()
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-qm.ctx.Done():
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(qm.ctx, 5*time.Minute)
					if err := reports.SendPending(ctx); err != nil {
						qm.logger.Error("TLS report delivery failed", zap.Error(err))
					}
					cancel()
				}
			}
		}()
	}
	qm.wg.Add(1)
	go func() {
		defer qm.wg.Done()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-qm.ctx.Done():
				return
			case <-ticker.C:
				if err := qm.store.Compact(time.Duration(cfg.Platform.QuarantineRetentionDays) * 24 * time.Hour); err != nil {
					qm.logger.Error("Storage compaction failed", zap.Error(err))
				}
			}
		}
	}()

	if cfg.Platform.MTASTS {
		m := security.NewMTASTSManager(qm.logger)
		if err := m.SetCachePath(filepath.Join(cfg.Platform.DataDir, "mta-sts.json")); err != nil {
			return err
		}
		qm.mailDelivery.SetMTASTS(m)
	}
	return nil
}
func messageFromEntry(entry *storage.JournalEntry) *Message {
	msg := &Message{}
	if raw := entry.Metadata["message"]; raw != "" {
		json.Unmarshal([]byte(raw), msg)
	}
	if msg.AdmissionKey == "" {
		ip := msg.ClientIP
		if ip == "" {
			ip = entry.Metadata["client_ip"]
		}
		msg.ClientIP = ip
		if ip != "" {
			msg.AdmissionKey = mailstorm.Identity("", ip)
		} else {
			msg.AdmissionKey = "unknown"
		}
	}
	msg.Quarantine = entry.Status == "held"
	msg.ID = entry.MessageID
	msg.From = entry.From
	msg.To = entry.To
	msg.Data = entry.Data
	msg.CreatedAt = entry.CreatedAt
	msg.Tier = QueueTier(entry.Tier)
	return msg
}
