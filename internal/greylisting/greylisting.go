package greylisting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Greylisting implements RFC-compliant greylisting for spam reduction
// Greylisting temporarily rejects mail from unknown (IP, FROM, TO) triplets
// Legitimate mail servers will retry, while spammers typically don't
type Greylisting struct {
	logger *zap.Logger

	// Triplet storage: hash(ip+from+to) -> greylistEntry
	triplets   map[string]*greylistEntry
	tripletsMu sync.RWMutex

	// Auto-whitelist for successful triplets
	whitelist   map[string]time.Time // hash -> expiry
	whitelistMu sync.RWMutex

	// Configuration
	retryDelay    time.Duration // Minimum time before retry is accepted (default 5 minutes)
	expiry        time.Duration // How long to remember a triplet (default 24 hours)
	whitelistTTL  time.Duration // How long to auto-whitelist after success (default 30 days)
}

type greylistEntry struct {
	ip          string
	from        string
	to          string
	firstSeen   time.Time
	lastAttempt time.Time
	attempts    int
	whitelisted bool
}

// NewGreylisting creates a new greylisting instance
func NewGreylisting(logger *zap.Logger) *Greylisting {
	return &Greylisting{
		logger:        logger,
		triplets:      make(map[string]*greylistEntry),
		whitelist:     make(map[string]time.Time),
		retryDelay:    5 * time.Minute,  // RFC recommendation: 5-10 minutes
		expiry:        24 * time.Hour,   // Remember for 24 hours
		whitelistTTL:  30 * 24 * time.Hour, // 30 days auto-whitelist
	}
}

// Check determines if a message should be greylisted
// Returns: (shouldGreylist bool, retryAfter time.Duration, error)
func (g *Greylisting) Check(ip, from, to string) (bool, time.Duration, error) {
	tripletHash := g.hashTriplet(ip, from, to)

	// Check whitelist first (fast path)
	g.whitelistMu.RLock()
	if expiry, whitelisted := g.whitelist[tripletHash]; whitelisted {
		if time.Now().Before(expiry) {
			g.whitelistMu.RUnlock()
			g.logger.Debug("Triplet is whitelisted",
				zap.String("ip", ip),
				zap.String("from", from),
				zap.String("to", to))
			return false, 0, nil
		}
		// Expired, remove from whitelist
		g.whitelistMu.RUnlock()
		g.whitelistMu.Lock()
		delete(g.whitelist, tripletHash)
		g.whitelistMu.Unlock()
	} else {
		g.whitelistMu.RUnlock()
	}

	// Check greylist triplets
	g.tripletsMu.Lock()
	defer g.tripletsMu.Unlock()

	entry, exists := g.triplets[tripletHash]
	now := time.Now()

	if !exists {
		// First time seeing this triplet - create entry and greylist
		entry = &greylistEntry{
			ip:          ip,
			from:        from,
			to:          to,
			firstSeen:   now,
			lastAttempt: now,
			attempts:    1,
			whitelisted: false,
		}
		g.triplets[tripletHash] = entry

		g.logger.Info("New triplet - greylisting",
			zap.String("ip", ip),
			zap.String("from", from),
			zap.String("to", to),
			zap.Duration("retry_after", g.retryDelay))

		return true, g.retryDelay, nil
	}

	// Update attempt tracking
	entry.lastAttempt = now
	entry.attempts++

	// Check if enough time has passed since first attempt
	timeSinceFirst := now.Sub(entry.firstSeen)

	if timeSinceFirst < g.retryDelay {
		// Still within greylist window
		remaining := g.retryDelay - timeSinceFirst
		g.logger.Info("Triplet still greylisted",
			zap.String("ip", ip),
			zap.String("from", from),
			zap.String("to", to),
			zap.Duration("time_since_first", timeSinceFirst),
			zap.Duration("retry_after", remaining))
		return true, remaining, nil
	}

	// Passed the greylist test - add to auto-whitelist
	g.whitelistMu.Lock()
	g.whitelist[tripletHash] = now.Add(g.whitelistTTL)
	g.whitelistMu.Unlock()

	entry.whitelisted = true

	g.logger.Info("Triplet passed greylist - auto-whitelisted",
		zap.String("ip", ip),
		zap.String("from", from),
		zap.String("to", to),
		zap.Int("attempts", entry.attempts),
		zap.Duration("time_to_retry", timeSinceFirst))

	return false, 0, nil
}

// hashTriplet creates a unique hash for the (ip, from, to) triplet
func (g *Greylisting) hashTriplet(ip, from, to string) string {
	// Normalize inputs
	key := fmt.Sprintf("%s|%s|%s", ip, from, to)
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// CleanupExpired removes expired entries from greylist and whitelist
func (g *Greylisting) CleanupExpired() {
	now := time.Now()

	// Clean triplets
	g.tripletsMu.Lock()
	for hash, entry := range g.triplets {
		if now.Sub(entry.lastAttempt) > g.expiry {
			delete(g.triplets, hash)
		}
	}
	tripletsRemoved := len(g.triplets)
	g.tripletsMu.Unlock()

	// Clean whitelist
	g.whitelistMu.Lock()
	whitelistRemoved := 0
	for hash, expiry := range g.whitelist {
		if now.After(expiry) {
			delete(g.whitelist, hash)
			whitelistRemoved++
		}
	}
	g.whitelistMu.Unlock()

	g.logger.Info("Greylisting cleanup complete",
		zap.Int("triplets_active", tripletsRemoved),
		zap.Int("whitelist_expired", whitelistRemoved))
}

// StartCleanupTimer starts a background goroutine to periodically clean up
func (g *Greylisting) StartCleanupTimer(interval time.Duration) chan struct{} {
	stopChan := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				g.logger.Info("Greylisting cleanup timer stopped")
				return
			case <-ticker.C:
				g.CleanupExpired()
			}
		}
	}()

	g.logger.Info("Greylisting cleanup timer started",
		zap.Duration("interval", interval))

	return stopChan
}

// GetStats returns greylisting statistics
func (g *Greylisting) GetStats() map[string]int {
	g.tripletsMu.RLock()
	tripletCount := len(g.triplets)
	whitelistedCount := 0
	for _, entry := range g.triplets {
		if entry.whitelisted {
			whitelistedCount++
		}
	}
	g.tripletsMu.RUnlock()

	g.whitelistMu.RLock()
	autoWhitelistCount := len(g.whitelist)
	g.whitelistMu.RUnlock()

	return map[string]int{
		"active_triplets":    tripletCount,
		"whitelisted":        whitelistedCount,
		"auto_whitelist":     autoWhitelistCount,
	}
}

// ManualWhitelist manually adds a triplet to the whitelist
func (g *Greylisting) ManualWhitelist(ip, from, to string) {
	tripletHash := g.hashTriplet(ip, from, to)

	g.whitelistMu.Lock()
	g.whitelist[tripletHash] = time.Now().Add(g.whitelistTTL)
	g.whitelistMu.Unlock()

	g.logger.Info("Triplet manually whitelisted",
		zap.String("ip", ip),
		zap.String("from", from),
		zap.String("to", to))
}

// RemoveFromWhitelist removes a triplet from the whitelist
func (g *Greylisting) RemoveFromWhitelist(ip, from, to string) {
	tripletHash := g.hashTriplet(ip, from, to)

	g.whitelistMu.Lock()
	delete(g.whitelist, tripletHash)
	g.whitelistMu.Unlock()

	g.logger.Info("Triplet removed from whitelist",
		zap.String("ip", ip),
		zap.String("from", from),
		zap.String("to", to))
}

// SetRetryDelay configures the minimum retry delay
func (g *Greylisting) SetRetryDelay(delay time.Duration) {
	g.retryDelay = delay
	g.logger.Info("Greylisting retry delay updated",
		zap.Duration("delay", delay))
}

// SetExpiry configures how long to remember triplets
func (g *Greylisting) SetExpiry(expiry time.Duration) {
	g.expiry = expiry
	g.logger.Info("Greylisting expiry updated",
		zap.Duration("expiry", expiry))
}

// SetWhitelistTTL configures auto-whitelist duration
func (g *Greylisting) SetWhitelistTTL(ttl time.Duration) {
	g.whitelistTTL = ttl
	g.logger.Info("Greylisting whitelist TTL updated",
		zap.Duration("ttl", ttl))
}
