package dns

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// Resolver provides DNS resolution with caching
type Resolver struct {
	lookupGroup  singleflight.Group
	negativeHits atomic.Uint64
	hits         atomic.Uint64
	misses       atomic.Uint64
	failures     atomic.Uint64
	mailServers  []string
	logger       *zap.Logger
	resolver     lookupResolver

	// Cache for MX records
	mxCache   map[string]*mxCacheEntry
	mxCacheMu sync.RWMutex

	// Cache for TXT records
	txtCache   map[string]*txtCacheEntry
	txtCacheMu sync.RWMutex

	// Configuration
	timeout  time.Duration
	cacheTTL time.Duration
}

type mxCacheEntry struct {
	err       error
	records   []*net.MX
	expiresAt time.Time
}

type txtCacheEntry struct {
	err       error
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
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	cached := func() ([]*net.MX, error, bool) {
		r.mxCacheMu.RLock()
		entry, ok := r.mxCache[domain]
		r.mxCacheMu.RUnlock()
		if ok && time.Now().Before(entry.expiresAt) {
			r.hits.Add(1)
			if entry.err != nil {
				r.negativeHits.Add(1)
			}
			return cloneMX(entry.records), cloneDNSError(entry.err), true
		}
		return nil, nil, false
	}
	if records, err, ok := cached(); ok {
		return records, err
	}
	result := r.lookupGroup.DoChan("MX:"+domain, func() (interface{}, error) {
		if records, err, ok := cached(); ok {
			return records, err
		}
		lookupCtx, cancel := context.WithTimeout(ctx, r.timeout)
		defer cancel()
		r.misses.Add(1)
		records, err := r.resolver.LookupMX(lookupCtx, domain)
		ttl := r.cacheTTL
		if err != nil {
			r.failures.Add(1)
			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound || dnsErr.IsTimeout || dnsErr.IsTemporary {
				return nil, err
			}
			ttl = 30 * time.Second
		}
		r.mxCacheMu.Lock()
		if len(r.mxCache) >= 4096 {
			for key := range r.mxCache {
				delete(r.mxCache, key)
				break
			}
		}
		r.mxCache[domain] = &mxCacheEntry{records: cloneMX(records), expiresAt: time.Now().Add(ttl), err: cloneDNSError(err)}
		r.mxCacheMu.Unlock()
		return records, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-result:
		if v.Err != nil {
			return nil, cloneDNSError(v.Err)
		}
		return cloneMX(v.Val.([]*net.MX)), nil
	}
}
func (r *Resolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	cached := func() ([]string, error, bool) {
		r.txtCacheMu.RLock()
		entry, ok := r.txtCache[domain]
		r.txtCacheMu.RUnlock()
		if ok && time.Now().Before(entry.expiresAt) {
			r.hits.Add(1)
			if entry.err != nil {
				r.negativeHits.Add(1)
			}
			return cloneTXT(entry.records), cloneDNSError(entry.err), true
		}
		return nil, nil, false
	}
	if records, err, ok := cached(); ok {
		return records, err
	}
	result := r.lookupGroup.DoChan("TXT:"+domain, func() (interface{}, error) {
		if records, err, ok := cached(); ok {
			return records, err
		}
		lookupCtx, cancel := context.WithTimeout(ctx, r.timeout)
		defer cancel()
		r.misses.Add(1)
		records, err := r.resolver.LookupTXT(lookupCtx, domain)
		ttl := r.cacheTTL
		if err != nil {
			r.failures.Add(1)
			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound || dnsErr.IsTimeout || dnsErr.IsTemporary {
				return nil, err
			}
			ttl = 30 * time.Second
		}
		r.txtCacheMu.Lock()
		if len(r.txtCache) >= 4096 {
			for key := range r.txtCache {
				delete(r.txtCache, key)
				break
			}
		}
		r.txtCache[domain] = &txtCacheEntry{records: cloneTXT(records), expiresAt: time.Now().Add(ttl), err: cloneDNSError(err)}
		r.txtCacheMu.Unlock()
		return records, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-result:
		if v.Err != nil {
			return nil, cloneDNSError(v.Err)
		}
		return cloneTXT(v.Val.([]string)), nil
	}
}
func cloneMX(records []*net.MX) []*net.MX {
	out := make([]*net.MX, len(records))
	for i, v := range records {
		if v != nil {
			copy := *v
			out[i] = &copy
		}
	}
	return out
}
func cloneTXT(records []string) []string { return append([]string(nil), records...) }
func cloneDNSError(err error) error {
	if e, ok := err.(*net.DNSError); ok {
		copy := *e
		return &copy
	}
	return err
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

func (r *Resolver) Statistics() map[string]interface{} {
	return map[string]interface{}{"cache": r.GetCacheStats(), "hits": r.hits.Load(), "misses": r.misses.Load(), "failures": r.failures.Load(), "negative_hits": r.negativeHits.Load(), "scope": "MX/TXT lookups"}
}

type lookupResolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupTXT(context.Context, string) ([]string, error)
	LookupAddr(context.Context, string) ([]string, error)
	LookupIP(context.Context, string, string) ([]net.IP, error)
}
