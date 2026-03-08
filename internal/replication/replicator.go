package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

// ReplicationMode defines how replication operates
type ReplicationMode string

const (
	ModePrimary   ReplicationMode = "primary"
	ModeSecondary ReplicationMode = "secondary"
	ModeStandby   ReplicationMode = "standby" // Read-only replica
)

// Replicator handles disaster recovery through message replication
type Replicator struct {
	logger *zap.Logger
	store  *storage.MessageStore
	mode   ReplicationMode

	// Replication peers
	peers   []string // Addresses of replica nodes
	peersMu sync.RWMutex

	// Replication state
	listener net.Listener
	conns    map[string]net.Conn
	connsMu  sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewReplicator creates a replication instance
func NewReplicator(store *storage.MessageStore, mode ReplicationMode, listenAddr string, peers []string, logger *zap.Logger) (*Replicator, error) {
	ctx, cancel := context.WithCancel(context.Background())

	r := &Replicator{
		logger: logger,
		store:  store,
		mode:   mode,
		peers:  peers,
		conns:  make(map[string]net.Conn),
		ctx:    ctx,
		cancel: cancel,
	}

	// Start listening for replication connections
	if mode == ModePrimary || mode == ModeSecondary {
		listener, err := net.Listen("tcp", listenAddr)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to start replication listener: %w", err)
		}
		r.listener = listener
		logger.Info("Replication listener started", zap.String("addr", listenAddr), zap.String("mode", string(mode)))
	}

	return r, nil
}

// Start begins replication operations
func (r *Replicator) Start() error {
	// Connect to all peers
	if r.mode == ModePrimary || r.mode == ModeSecondary {
		for _, peer := range r.peers {
			go r.connectToPeer(peer)
		}
	}

	// Accept incoming replication connections
	if r.listener != nil {
		r.wg.Add(1)
		go r.acceptConnections()
	}

	r.logger.Info("Replicator started", zap.String("mode", string(r.mode)))
	return nil
}

// ReplicateEntry sends a journal entry to all connected replicas
func (r *Replicator) ReplicateEntry(entry *storage.JournalEntry) error {
	if r.mode != ModePrimary {
		return fmt.Errorf("only primary can replicate entries")
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	r.connsMu.RLock()
	defer r.connsMu.RUnlock()

	var lastErr error
	successCount := 0

	for peer, conn := range r.conns {
		// Send length-prefixed message
		msg := fmt.Sprintf("%d\n%s\n", len(data), data)
		if _, err := conn.Write([]byte(msg)); err != nil {
			r.logger.Error("Failed to replicate to peer", zap.String("peer", peer), zap.Error(err))
			lastErr = err
			continue
		}
		successCount++
	}

	if successCount == 0 && len(r.conns) > 0 {
		return fmt.Errorf("failed to replicate to any peer: %w", lastErr)
	}

	r.logger.Debug("Replicated entry", zap.String("msg_id", entry.MessageID), zap.Int("replicas", successCount))
	return nil
}

// connectToPeer establishes connection to a replication peer
func (r *Replicator) connectToPeer(peer string) {
	backoff := time.Second

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", peer, 5*time.Second)
		if err != nil {
			r.logger.Warn("Failed to connect to peer, retrying",
				zap.String("peer", peer),
				zap.Error(err),
				zap.Duration("backoff", backoff))
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}

		r.logger.Info("Connected to replication peer", zap.String("peer", peer))

		r.connsMu.Lock()
		r.conns[peer] = conn
		r.connsMu.Unlock()

		// Reset backoff on successful connection
		backoff = time.Second

		// Monitor connection
		r.monitorConnection(peer, conn)

		// Connection lost, cleanup and retry
		r.connsMu.Lock()
		delete(r.conns, peer)
		r.connsMu.Unlock()

		conn.Close()
		r.logger.Warn("Lost connection to peer", zap.String("peer", peer))
	}
}

// monitorConnection checks if connection is still alive
func (r *Replicator) monitorConnection(peer string, conn net.Conn) {
	buf := make([]byte, 1)
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				r.logger.Debug("Connection monitor error", zap.String("peer", peer), zap.Error(err))
			}
			return
		}
	}
}

// acceptConnections handles incoming replication connections
func (r *Replicator) acceptConnections() {
	defer r.wg.Done()

	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.ctx.Done():
				return
			default:
				r.logger.Error("Failed to accept connection", zap.Error(err))
				continue
			}
		}

		r.logger.Info("Accepted replication connection", zap.String("remote", conn.RemoteAddr().String()))

		r.wg.Add(1)
		go r.handleReplicationStream(conn)
	}
}

// handleReplicationStream receives and applies replicated entries
func (r *Replicator) handleReplicationStream(conn net.Conn) {
	defer r.wg.Done()
	defer conn.Close()

	decoder := json.NewDecoder(conn)

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		var entry storage.JournalEntry
		if err := decoder.Decode(&entry); err != nil {
			if err == io.EOF {
				r.logger.Info("Replication stream closed", zap.String("remote", conn.RemoteAddr().String()))
				return
			}
			r.logger.Error("Failed to decode replicated entry", zap.Error(err))
			return
		}

		// Apply replicated entry to local store
		if err := r.applyReplicatedEntry(&entry); err != nil {
			r.logger.Error("Failed to apply replicated entry",
				zap.String("msg_id", entry.MessageID),
				zap.Error(err))
		}
	}
}

// applyReplicatedEntry stores a replicated entry
func (r *Replicator) applyReplicatedEntry(entry *storage.JournalEntry) error {
	// Store in local journal for disaster recovery
	_, isDup, err := r.store.Store(entry)
	if err != nil {
		return err
	}

	if isDup {
		r.logger.Debug("Received duplicate replicated entry", zap.String("msg_id", entry.MessageID))
	} else {
		r.logger.Debug("Applied replicated entry", zap.String("msg_id", entry.MessageID))
	}

	return nil
}

// PromoteToPrimary promotes a secondary/standby to primary (for failover)
func (r *Replicator) PromoteToPrimary() error {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	if r.mode == ModePrimary {
		return fmt.Errorf("already in primary mode")
	}

	r.mode = ModePrimary
	r.logger.Info("Promoted to primary mode")

	// Start connecting to peers as primary
	for _, peer := range r.peers {
		go r.connectToPeer(peer)
	}

	return nil
}

// GetMode returns the current replication mode
func (r *Replicator) GetMode() ReplicationMode {
	return r.mode
}

// GetPeerStatus returns status of connected peers
func (r *Replicator) GetPeerStatus() map[string]bool {
	r.connsMu.RLock()
	defer r.connsMu.RUnlock()

	status := make(map[string]bool)
	for _, peer := range r.peers {
		_, connected := r.conns[peer]
		status[peer] = connected
	}

	return status
}

// Shutdown gracefully stops the replicator
func (r *Replicator) Shutdown() error {
	r.logger.Info("Shutting down replicator")
	r.cancel()

	if r.listener != nil {
		r.listener.Close()
	}

	r.connsMu.Lock()
	for _, conn := range r.conns {
		conn.Close()
	}
	r.connsMu.Unlock()

	r.wg.Wait()
	r.logger.Info("Replicator stopped")
	return nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
