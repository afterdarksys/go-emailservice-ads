package smtpd

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// RetryPolicy defines the retry behavior for failed messages
type RetryPolicy struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	PermanentErrors map[int]bool // SMTP codes that should not be retried
}

// DefaultRetryPolicy returns a sensible retry policy for email delivery
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:   5,
		InitialDelay:  1 * time.Minute,
		MaxDelay:      4 * time.Hour,
		BackoffFactor: 2.0,
		PermanentErrors: map[int]bool{
			550: true, // Mailbox unavailable
			551: true, // User not local
			552: true, // Exceeded storage allocation
			553: true, // Mailbox name not allowed
			554: true, // Transaction failed
		},
	}
}

// RetryScheduler handles automatic retry of failed messages
type RetryScheduler struct {
	store  *storage.MessageStore
	qm     *QueueManager
	policy *RetryPolicy
	logger *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

// NewRetryScheduler creates a retry scheduler
func NewRetryScheduler(store *storage.MessageStore, qm *QueueManager, policy *RetryPolicy, logger *zap.Logger) *RetryScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &RetryScheduler{
		store:  store,
		qm:     qm,
		policy: policy,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the retry scheduler background process
func (rs *RetryScheduler) Start() {
	go rs.retryLoop()
	rs.logger.Info("Retry scheduler started")
}

// retryLoop periodically checks for messages that need retry
func (rs *RetryScheduler) retryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rs.ctx.Done():
			rs.logger.Info("Retry scheduler stopped")
			return
		case <-ticker.C:
			rs.processRetries()
		}
	}
}

// processRetries finds and retries eligible messages
func (rs *RetryScheduler) processRetries() {
	pending := rs.store.ListPending("")

	for _, entry := range pending {
		if entry.Status != "pending" || entry.Attempts >= rs.policy.MaxAttempts {
			continue
		}

		// Calculate next retry time using exponential backoff
		nextRetry := rs.calculateNextRetry(entry.Attempts, entry.CreatedAt)
		if time.Now().Before(nextRetry) {
			continue // Not ready for retry yet
		}

		// Convert storage entry back to queue message
		msg := &Message{
			ID:        entry.MessageID,
			From:      entry.From,
			To:        entry.To,
			Data:      entry.Data,
			CreatedAt: entry.CreatedAt,
			Tier:      QueueTier(entry.Tier),
		}

		rs.logger.Info("Retrying message",
			zap.String("msg_id", entry.MessageID),
			zap.Int("attempt", entry.Attempts+1),
			zap.Int("max_attempts", rs.policy.MaxAttempts))

		// Update status before requeue
		if err := rs.store.UpdateStatus(entry.MessageID, "processing", ""); err != nil {
			rs.logger.Error("Failed to update message status", zap.Error(err))
			continue
		}

		// Re-enqueue for processing
		rs.qm.Enqueue(msg)
	}
}

// calculateNextRetry computes the next retry time using exponential backoff
func (rs *RetryScheduler) calculateNextRetry(attempts int, createdAt time.Time) time.Time {
	delay := float64(rs.policy.InitialDelay) * math.Pow(rs.policy.BackoffFactor, float64(attempts))

	if delay > float64(rs.policy.MaxDelay) {
		delay = float64(rs.policy.MaxDelay)
	}

	return createdAt.Add(time.Duration(delay))
}

// ShouldRetry determines if a message should be retried based on error
func (rs *RetryScheduler) ShouldRetry(smtpCode int, attempts int) (bool, string) {
	// Check if it's a permanent error
	if rs.policy.PermanentErrors[smtpCode] {
		return false, fmt.Sprintf("permanent error: SMTP %d", smtpCode)
	}

	// Check if max attempts exceeded
	if attempts >= rs.policy.MaxAttempts {
		return false, fmt.Sprintf("max attempts exceeded: %d/%d", attempts, rs.policy.MaxAttempts)
	}

	return true, ""
}

// Shutdown gracefully stops the retry scheduler
func (rs *RetryScheduler) Shutdown() {
	rs.logger.Info("Shutting down retry scheduler")
	rs.cancel()
}
