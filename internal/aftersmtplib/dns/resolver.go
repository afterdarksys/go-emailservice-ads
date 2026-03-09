package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Resolver provides DNS resolution with caching
type Resolver struct {
	logger    *zap.Logger
	resolver  *net.Resolver

	// Cache for MX records
	mxCache   map[string]*mxCacheEntry
	mxCacheMu sync.RWMutex

	// Cache for TXT records
	txtCache   map[string]*txtCacheEntry
	txtCacheMu sync.RWMutex

	// Configuration
	timeout    time.Duration
	cacheTTL   time.Duration
}

type mxCacheEntry struct {
	records   []*net.MX
	expiresAt time.Time
}

type txtCacheEntry struct {
	records   []string
	expiresAt time.Time
}

// NewResolver creates a new DNS resolver with caching
func NewResolver(logger *zap.Logger) *Resolver {
	return &Resolver{
		logger: logger,
		resolver: &net.Resolver{
			PreferGo: true, // Use Go's DNS resolver for better control
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 10 * time.Second,
				}
				return d.DialContext(ctx, network, address)
			},
		},
		mxCache:  make(map[string]*mxCacheEntry),
		txtCache: make(map[string]*txtCacheEntry),
		timeout:  10 * time.Second,
		cacheTTL: 5 * time.Minute, // Cache DNS records for 5 minutes
	}
}

// LookupMX performs MX record lookup with caching
// RFC 5321 Section 5 - Address Resolution
func (r *Resolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	// Check cache first
	r.mxCacheMu.RLock()
	if entry, exists := r.mxCache[domain]; exists {
		if time.Now().Before(entry.expiresAt) {
			r.mxCacheMu.RUnlock()
			r.logger.Debug("MX cache hit", zap.String("domain", domain))
			return entry.records, nil
		}
	}
	r.mxCacheMu.RUnlock()

	// Cache miss or expired - perform lookup
	r.logger.Debug("MX cache miss, performing lookup", zap.String("domain", domain))

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	records, err := r.resolver.LookupMX(ctx, domain)
	if err != nil {
		r.logger.Warn("MX lookup failed",
			zap.String("domain", domain),
			zap.Error(err))
		return nil, err
	}

	// Update cache
	r.mxCacheMu.Lock()
	r.mxCache[domain] = &mxCacheEntry{
		records:   records,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
	r.mxCacheMu.Unlock()

	r.logger.Debug("MX lookup successful",
		zap.String("domain", domain),
		zap.Int("mx_count", len(records)))

	return records, nil
}

// LookupTXT performs TXT record lookup with caching
// Used for SPF, DKIM, and DMARC lookups
func (r *Resolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	// Check cache first
	r.txtCacheMu.RLock()
	if entry, exists := r.txtCache[domain]; exists {
		if time.Now().Before(entry.expiresAt) {
			r.txtCacheMu.RUnlock()
			r.logger.Debug("TXT cache hit", zap.String("domain", domain))
			return entry.records, nil
		}
	}
	r.txtCacheMu.RUnlock()

	// Cache miss or expired - perform lookup
	r.logger.Debug("TXT cache miss, performing lookup", zap.String("domain", domain))

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	records, err := r.resolver.LookupTXT(ctx, domain)
	if err != nil {
		r.logger.Warn("TXT lookup failed",
			zap.String("domain", domain),
			zap.Error(err))
		return nil, err
	}

	// Update cache
	r.txtCacheMu.Lock()
	r.txtCache[domain] = &txtCacheEntry{
		records:   records,
		expiresAt: time.Now().Add(r.cacheTTL),
	}
	r.txtCacheMu.Unlock()

	r.logger.Debug("TXT lookup successful",
		zap.String("domain", domain),
		zap.Int("record_count", len(records)))

	return records, nil
}

// LookupAddr performs reverse DNS lookup (PTR record)
// Used for IP validation
func (r *Resolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	names, err := r.resolver.LookupAddr(ctx, addr)
	if err != nil {
		r.logger.Debug("PTR lookup failed",
			zap.String("addr", addr),
			zap.Error(err))
		return nil, err
	}

	r.logger.Debug("PTR lookup successful",
		zap.String("addr", addr),
		zap.Strings("names", names))

	return names, nil
}

// LookupIP performs forward DNS lookup (A/AAAA records)
func (r *Resolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	ips, err := r.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		r.logger.Debug("IP lookup failed",
			zap.String("host", host),
			zap.Error(err))
		return nil, err
	}

	r.logger.Debug("IP lookup successful",
		zap.String("host", host),
		zap.Int("ip_count", len(ips)))

	return ips, nil
}

// ClearCache clears all cached DNS records
func (r *Resolver) ClearCache() {
	r.mxCacheMu.Lock()
	r.mxCache = make(map[string]*mxCacheEntry)
	r.mxCacheMu.Unlock()

	r.txtCacheMu.Lock()
	r.txtCache = make(map[string]*txtCacheEntry)
	r.txtCacheMu.Unlock()

	r.logger.Info("DNS cache cleared")
}

// GetCacheStats returns cache statistics
func (r *Resolver) GetCacheStats() map[string]int {
	r.mxCacheMu.RLock()
	mxCount := len(r.mxCache)
	r.mxCacheMu.RUnlock()

	r.txtCacheMu.RLock()
	txtCount := len(r.txtCache)
	r.txtCacheMu.RUnlock()

	return map[string]int{
		"mx_records":  mxCount,
		"txt_records": txtCount,
	}
}

// StartCacheCleanup starts a background goroutine to clean expired cache entries
func (r *Resolver) StartCacheCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.cleanupExpiredEntries()
			}
		}
	}()
}

// cleanupExpiredEntries removes expired cache entries
func (r *Resolver) cleanupExpiredEntries() {
	now := time.Now()

	// Clean MX cache
	r.mxCacheMu.Lock()
	for domain, entry := range r.mxCache {
		if now.After(entry.expiresAt) {
			delete(r.mxCache, domain)
		}
	}
	r.mxCacheMu.Unlock()

	// Clean TXT cache
	r.txtCacheMu.Lock()
	for domain, entry := range r.txtCache {
		if now.After(entry.expiresAt) {
			delete(r.txtCache, domain)
		}
	}
	r.txtCacheMu.Unlock()
}
