package state

import (
	"context"
	"time"
)

// Store defines the interface for cluster state storage
// Implementations: etcd, Redis, Consul
type Store interface {
	// Key-Value Operations
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) (map[string][]byte, error)

	// Atomic Operations
	CompareAndSwap(ctx context.Context, key string, oldValue, newValue []byte) (bool, error)
	PutWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Distributed Locking
	Lock(ctx context.Context, key string, ttl time.Duration) (LockHandle, error)
	Unlock(ctx context.Context, handle LockHandle) error

	// Watch for Changes
	Watch(ctx context.Context, prefix string) (<-chan WatchEvent, error)

	// Leadership Election
	Campaign(ctx context.Context, election string, value string, ttl time.Duration) (<-chan bool, error)
	Resign(ctx context.Context, election string) error

	// Health Check
	Ping(ctx context.Context) error

	// Close the store
	Close() error
}

// LockHandle represents a distributed lock
type LockHandle interface {
	Key() string
	Release(ctx context.Context) error
}

// WatchEvent represents a change in the store
type WatchEvent struct {
	Type  WatchEventType
	Key   string
	Value []byte
}

// WatchEventType defines the type of watch event
type WatchEventType int

const (
	WatchEventPut WatchEventType = iota
	WatchEventDelete
)

// Config contains common configuration for all store backends
type Config struct {
	Type      string        // "etcd", "redis", "consul"
	Endpoints []string      // Server addresses
	Timeout   time.Duration // Default operation timeout
	TLS       *TLSConfig    // TLS configuration (optional)
}

// TLSConfig contains TLS settings for secure connections
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// NewStore creates a new state store based on the configuration
func NewStore(config *Config) (Store, error) {
	switch config.Type {
	case "etcd":
		return NewEtcdStore(config)
	case "redis":
		return NewRedisStore(config)
	case "consul":
		return NewConsulStore(config)
	default:
		return nil, ErrUnknownStoreType
	}
}

// Common errors
var (
	ErrUnknownStoreType = &StoreError{Code: "unknown_store_type", Message: "unknown state store type"}
	ErrKeyNotFound      = &StoreError{Code: "key_not_found", Message: "key not found"}
	ErrLockFailed       = &StoreError{Code: "lock_failed", Message: "failed to acquire lock"}
	ErrCASFailed        = &StoreError{Code: "cas_failed", Message: "compare-and-swap failed"}
)

// StoreError represents a state store error
type StoreError struct {
	Code    string
	Message string
	Err     error
}

func (e *StoreError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *StoreError) Unwrap() error {
	return e.Err
}
