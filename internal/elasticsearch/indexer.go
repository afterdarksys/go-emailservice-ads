package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8/esutil"
	"go.uber.org/zap"
)

// Indexer handles async bulk indexing of mail events to Elasticsearch
type Indexer struct {
	client        *Client
	logger        *zap.Logger
	bulkIndexer   esutil.BulkIndexer
	events        chan *MailEvent
	stats         *IndexStats
	statsMu       sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	samplingRate  float64
}

// NewIndexer creates a new Elasticsearch bulk indexer
func NewIndexer(client *Client, logger *zap.Logger) (*Indexer, error) {
	cfg := client.config.Elasticsearch

	// Create bulk indexer
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        client.es,
		NumWorkers:    cfg.Workers,
		FlushBytes:    5 * 1024 * 1024, // 5MB
		FlushInterval: parseDuration(cfg.FlushInterval, 5*time.Second),
		OnError: func(ctx context.Context, err error) {
			logger.Error("Bulk indexer error", zap.Error(err))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bulk indexer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	indexer := &Indexer{
		client:       client,
		logger:       logger,
		bulkIndexer:  bi,
		events:       make(chan *MailEvent, cfg.BulkSize*2), // Buffer 2x bulk size
		stats:        &IndexStats{},
		ctx:          ctx,
		cancel:       cancel,
		samplingRate: cfg.SamplingRate,
	}

	// Start event processor
	indexer.start()

	logger.Info("Elasticsearch indexer started",
		zap.Int("workers", cfg.Workers),
		zap.String("flush_interval", cfg.FlushInterval),
		zap.Float64("sampling_rate", cfg.SamplingRate))

	return indexer, nil
}

// start begins processing events from the channel
func (idx *Indexer) start() {
	idx.wg.Add(1)
	go func() {
		defer idx.wg.Done()

		for {
			select {
			case <-idx.ctx.Done():
				idx.logger.Info("Indexer shutting down")
				return
			case event := <-idx.events:
				idx.processEvent(event)
			}
		}
	}()
}

// PublishEvent publishes a mail event to Elasticsearch (non-blocking)
func (idx *Indexer) PublishEvent(event *MailEvent) {
	// Apply sampling
	if idx.samplingRate < 1.0 && rand.Float64() > idx.samplingRate {
		idx.updateStats(0, 0, 1)
		return
	}

	// Extract headers if enabled and configured
	if idx.client.shouldLogHeaders(event) {
		// Note: In real implementation, we'd need access to raw message data
		// This would be passed in the event or retrieved from storage
		// For now, headers will be added by the caller if available
	}

	select {
	case idx.events <- event:
		// Event queued successfully
	default:
		// Channel full, drop event (or could block)
		idx.logger.Warn("Event queue full, dropping event",
			zap.String("message_id", event.MessageID),
			zap.String("event_type", string(event.EventType)))
		idx.updateStats(0, 0, 1)
	}
}

// processEvent processes a single event and adds it to the bulk indexer
func (idx *Indexer) processEvent(event *MailEvent) {
	// Ensure timestamp is set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		idx.logger.Error("Failed to marshal event",
			zap.String("message_id", event.MessageID),
			zap.Error(err))
		idx.updateStats(0, 1, 0)
		return
	}

	// Get index name (time-based)
	indexName := idx.client.GetIndexNameForTime(event.Timestamp)

	// Add to bulk indexer
	err = idx.bulkIndexer.Add(
		idx.ctx,
		esutil.BulkIndexerItem{
			Action:     "index",
			Index:      indexName,
			DocumentID: fmt.Sprintf("%s-%s", event.MessageID, event.EventType),
			Body:       bytes.NewReader(data),
			OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem) {
				idx.updateStats(int64(len(data)), 0, 0)
			},
			OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
				idx.logger.Error("Failed to index event",
					zap.String("message_id", event.MessageID),
					zap.String("event_type", string(event.EventType)),
					zap.String("error", res.Error.Reason),
					zap.Error(err))
				idx.updateStats(0, 1, 0)
			},
		},
	)

	if err != nil {
		idx.logger.Error("Failed to add event to bulk indexer",
			zap.String("message_id", event.MessageID),
			zap.Error(err))
		idx.updateStats(0, 1, 0)
	}
}

// Flush flushes any pending events
func (idx *Indexer) Flush() error {
	return idx.bulkIndexer.Close(idx.ctx)
}

// Close gracefully shuts down the indexer
func (idx *Indexer) Close() error {
	idx.logger.Info("Closing Elasticsearch indexer")
	idx.cancel()
	idx.wg.Wait()

	// Flush any remaining items
	if err := idx.bulkIndexer.Close(context.Background()); err != nil {
		return fmt.Errorf("failed to close bulk indexer: %w", err)
	}

	// Log final stats
	stats := idx.GetStats()
	idx.logger.Info("Indexer closed",
		zap.Int64("events_indexed", stats.EventsIndexed),
		zap.Int64("events_failed", stats.EventsFailed),
		zap.Int64("events_dropped", stats.EventsDropped),
		zap.Int64("bytes_indexed", stats.BytesIndexed))

	return nil
}

// GetStats returns current indexing statistics
func (idx *Indexer) GetStats() IndexStats {
	idx.statsMu.RLock()
	defer idx.statsMu.RUnlock()

	return IndexStats{
		EventsIndexed: idx.stats.EventsIndexed,
		EventsFailed:  idx.stats.EventsFailed,
		EventsDropped: idx.stats.EventsDropped,
		BytesIndexed:  idx.stats.BytesIndexed,
		LastIndexedAt: idx.stats.LastIndexedAt,
	}
}

// updateStats updates indexing statistics
func (idx *Indexer) updateStats(bytesIndexed int64, failed int64, dropped int64) {
	idx.statsMu.Lock()
	defer idx.statsMu.Unlock()

	if bytesIndexed > 0 {
		idx.stats.EventsIndexed++
		idx.stats.BytesIndexed += bytesIndexed
		idx.stats.LastIndexedAt = time.Now()
	}
	if failed > 0 {
		idx.stats.EventsFailed += failed
	}
	if dropped > 0 {
		idx.stats.EventsDropped += dropped
	}
}

// Helper function to parse duration string
func parseDuration(s string, defaultDuration time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration
	}
	return d
}

// BuildEvent creates a MailEvent from message components
func BuildEvent(
	eventType EventType,
	messageID string,
	traceID string,
	instanceID string,
	from string,
	to []string,
	tier string,
) *MailEvent {
	return &MailEvent{
		MessageID:  messageID,
		TraceID:    traceID,
		InstanceID: instanceID,
		EventType:  eventType,
		Timestamp:  time.Now(),
		Tier:       tier,
		Envelope: EnvelopeInfo{
			From: from,
			To:   to,
		},
		Metadata: MessageMetadata{},
		Security: SecurityInfo{},
		Delivery: DeliveryInfo{},
		Policy:   PolicyInfo{},
		Error:    ErrorInfo{},
	}
}

// SetEnvelope sets envelope information
func (e *MailEvent) SetEnvelope(from string, to []string, sizeBytes int) {
	e.Envelope = EnvelopeInfo{
		From:      from,
		To:        to,
		SizeBytes: sizeBytes,
	}
}

// SetMetadata sets message metadata
func (e *MailEvent) SetMetadata(contentHash, clientIP, authUser, heloHost string) {
	e.Metadata = MessageMetadata{
		ContentHash:       contentHash,
		ClientIP:          clientIP,
		AuthenticatedUser: authUser,
		HeloHostname:      heloHost,
		ReceivedAt:        time.Now(),
	}
}

// SetSecurity sets security check results
func (e *MailEvent) SetSecurity(spf, dkim, dmarc string, daneVerified bool, tlsVersion string, greylisted bool) {
	e.Security = SecurityInfo{
		SPFResult:    spf,
		DKIMResult:   dkim,
		DMARCResult:  dmarc,
		DANEVerified: daneVerified,
		TLSVersion:   tlsVersion,
		Greylisted:   greylisted,
	}
}

// SetDelivery sets delivery information
func (e *MailEvent) SetDelivery(remoteHost, remoteIP string, smtpCode int, latencyMs int64, attemptNum int, isPermanent bool) {
	e.Delivery = DeliveryInfo{
		RemoteHost:    remoteHost,
		RemoteIP:      remoteIP,
		SMTPCode:      smtpCode,
		LatencyMs:     latencyMs,
		AttemptNumber: attemptNum,
		IsPermanent:   isPermanent,
	}
}

// SetPolicy sets policy information
func (e *MailEvent) SetPolicy(policies []string, action string, score float64) {
	e.Policy = PolicyInfo{
		PoliciesApplied: policies,
		PolicyAction:    action,
		PolicyScore:     score,
	}
}

// SetError sets error information
func (e *MailEvent) SetError(message, code, category string, retryable bool) {
	e.Error = ErrorInfo{
		Message:   message,
		Code:      code,
		Category:  category,
		Retryable: retryable,
	}
}

// SetHeaders sets message headers (if header logging is enabled)
func (e *MailEvent) SetHeaders(headers map[string][]string) {
	e.Headers = headers
}

// SetRegion sets region and deployment mode
func (e *MailEvent) SetRegion(region, deploymentMode string) {
	e.Region = region
	e.DeploymentMode = deploymentMode
}

// SetParentTrace sets parent trace ID for related messages
func (e *MailEvent) SetParentTrace(parentTraceID string) {
	e.ParentTraceID = parentTraceID
}

// GetEventSummary returns a human-readable summary of the event
func (e *MailEvent) GetEventSummary() string {
	return fmt.Sprintf("[%s] %s: %s -> %s (%s)",
		e.EventType,
		e.MessageID,
		e.Envelope.From,
		strings.Join(e.Envelope.To, ","),
		e.Tier)
}
