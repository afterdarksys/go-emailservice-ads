package security

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SpreadPrevention implements a replica of Outbreak filters to detect
// burst patterns and cluster analysis for zero-day threat prevention.
type SpreadPrevention struct {
	logger        *zap.Logger
	burstWindow   time.Duration
	threshold     int
	mu            sync.RWMutex
	clusterHashes map[string][]time.Time
}

// NewSpreadPrevention initializes the Spread Prevention engine.
// burstWindow is the time frame to evaluate (e.g., 5m).
// threshold is the number of similar messages before triggering an outbreak.
func NewSpreadPrevention(logger *zap.Logger, burstWindow time.Duration, threshold int) *SpreadPrevention {
	sp := &SpreadPrevention{
		logger:        logger.With(zap.String("component", "spread_prevention")),
		burstWindow:   burstWindow,
		threshold:     threshold,
		clusterHashes: make(map[string][]time.Time),
	}
	
	// Start cleanup routine
	go sp.cleanupTask()
	
	return sp
}

// Evaluate checks if the given email matches an active outbreak cluster.
// Returns true if the message should be quarantined/held.
func (sp *SpreadPrevention) Evaluate(body []byte) bool {
	// 1. Cluster Analysis: Generate a hash signature for the email
	// In a real scenario, this would be a fuzzy hash (like SSDEEP/TLSH) 
	// or stripping standard headers/greetings. We use SHA256 of body for simplicity.
	hashBytes := sha256.Sum256(body)
	clusterID := hex.EncodeToString(hashBytes[:])

	sp.mu.Lock()
	defer sp.mu.Unlock()

	now := time.Now()
	
	// 2. Burst Detection: Track occurrences in the sliding window
	timestamps := sp.clusterHashes[clusterID]
	
	// Filter out expired timestamps
	validTimestamps := make([]time.Time, 0, len(timestamps)+1)
	for _, t := range timestamps {
		if now.Sub(t) <= sp.burstWindow {
			validTimestamps = append(validTimestamps, t)
		}
	}
	
	// Record this occurrence
	validTimestamps = append(validTimestamps, now)
	sp.clusterHashes[clusterID] = validTimestamps

	// 3. Outbreak Trigger
	if len(validTimestamps) >= sp.threshold {
		sp.logger.Warn("Spread Prevention triggered (Outbreak detected)",
			zap.String("cluster_id", clusterID),
			zap.Int("count", len(validTimestamps)),
			zap.Duration("window", sp.burstWindow))
		return true // Quarantine/Hold this message
	}

	return false
}

// cleanupTask periodically removes old tracking data
func (sp *SpreadPrevention) cleanupTask() {
	ticker := time.NewTicker(sp.burstWindow)
	for range ticker.C {
		sp.mu.Lock()
		now := time.Now()
		for clusterID, timestamps := range sp.clusterHashes {
			validTimestamps := make([]time.Time, 0, len(timestamps))
			for _, t := range timestamps {
				if now.Sub(t) <= sp.burstWindow {
					validTimestamps = append(validTimestamps, t)
				}
			}
			if len(validTimestamps) == 0 {
				delete(sp.clusterHashes, clusterID)
			} else {
				sp.clusterHashes[clusterID] = validTimestamps
			}
		}
		sp.mu.Unlock()
	}
}
