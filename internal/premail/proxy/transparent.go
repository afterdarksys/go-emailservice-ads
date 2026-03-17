package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/premail/analyzer"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/scoring"
)

// TransparentProxy is a transparent SMTP proxy that analyzes connections
// and forwards clean traffic to backend mail servers
type TransparentProxy struct {
	logger       *zap.Logger
	config       *Config
	analyzer     *analyzer.Analyzer
	scoringEngine *scoring.Engine
	nftables     NFTablesManager

	// Connection tracking
	activeConns  map[string]*Connection
	connMu       sync.RWMutex

	// Connection limit semaphore (prevents DOS)
	connSemaphore chan struct{}

	// Backend servers
	backends     []string
	backendIndex int
	backendMu    sync.Mutex

	// Metrics
	metrics      *Metrics
}

// Config holds configuration for the transparent proxy
type Config struct {
	ListenAddr         string
	BackendServers     []string
	ServerName         string
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	MaxConnections     int
	PreBannerTimeout   time.Duration
}

// NFTablesManager defines interface for nftables integration
type NFTablesManager interface {
	AddToBlacklist(ip net.IP, duration time.Duration) error
	AddToRatelimit(ip net.IP, duration time.Duration) error
	AddToMonitor(ip net.IP, duration time.Duration) error
	MarkPacket(ip net.IP, mark uint32) error
	RemoveFromBlacklist(ip net.IP) error
}

// Metrics tracks proxy statistics
type Metrics struct {
	TotalConnections    int64
	ActiveConnections   int64
	AllowedConnections  int64
	DroppedConnections  int64
	TarpitConnections   int64
	ThrottleConnections int64
	MonitorConnections  int64
	BackendForwards     int64
	BackendFailures     int64

	mu sync.RWMutex
}

// Connection represents an active SMTP connection
type Connection struct {
	ID              string
	RemoteAddr      net.IP
	LocalAddr       net.Addr
	ConnectedAt     time.Time
	DisconnectedAt  *time.Time

	// Client and backend connections
	ClientConn      net.Conn
	BackendConn     net.Conn

	// SMTP state
	State           SMTPState
	HeloReceived    bool
	HeloValue       string
	Authenticated   bool
	MessageCount    int

	// Metrics for scoring
	Metrics         *scoring.ConnectionMetrics

	// Scoring decision
	Decision        *scoring.ScoringDecision

	// Timing tracking
	CommandTimings  []time.Duration
	LastCommandTime time.Time
}

// SMTPState represents the current state of SMTP conversation
type SMTPState int

const (
	StateConnected SMTPState = iota
	StateHelo
	StateMailFrom
	StateRcptTo
	StateData
	StateQuit
)

// NewTransparentProxy creates a new transparent SMTP proxy
func NewTransparentProxy(
	config *Config,
	logger *zap.Logger,
	analyzer *analyzer.Analyzer,
	scoringEngine *scoring.Engine,
	nftables NFTablesManager,
) *TransparentProxy {
	// Set default max connections if not configured
	maxConns := config.MaxConnections
	if maxConns <= 0 {
		maxConns = 10000 // Default to 10k concurrent connections
		logger.Warn("MaxConnections not configured, using default",
			zap.Int("default_max", maxConns))
	}

	return &TransparentProxy{
		logger:        logger,
		config:        config,
		analyzer:      analyzer,
		scoringEngine: scoringEngine,
		nftables:      nftables,
		activeConns:   make(map[string]*Connection),
		connSemaphore: make(chan struct{}, maxConns), // Buffered channel acts as semaphore
		backends:      config.BackendServers,
		metrics:       &Metrics{},
	}
}

// Start begins listening for SMTP connections
func (p *TransparentProxy) Start() error {
	listener, err := net.Listen("tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.config.ListenAddr, err)
	}

	p.logger.Info("ADS PreMail transparent proxy started",
		zap.String("listen_addr", p.config.ListenAddr),
		zap.Strings("backends", p.backends))

	go p.acceptConnections(listener)

	return nil
}

// acceptConnections accepts incoming connections
func (p *TransparentProxy) acceptConnections(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			p.logger.Error("Failed to accept connection", zap.Error(err))
			continue
		}

		// Try to acquire connection slot (non-blocking)
		select {
		case p.connSemaphore <- struct{}{}:
			// Slot acquired, track metrics
			p.metrics.mu.Lock()
			p.metrics.TotalConnections++
			p.metrics.ActiveConnections++
			p.metrics.mu.Unlock()

			// Handle connection in goroutine
			go p.handleConnection(conn)
		default:
			// Connection limit reached, reject immediately
			p.logger.Warn("Connection limit reached, rejecting",
				zap.String("remote_addr", conn.RemoteAddr().String()),
				zap.Int("max_conns", p.config.MaxConnections))

			p.metrics.mu.Lock()
			p.metrics.DroppedConnections++
			p.metrics.mu.Unlock()

			conn.Close()
		}
	}
}

// handleConnection processes a single SMTP connection
func (p *TransparentProxy) handleConnection(clientConn net.Conn) {
	defer func() {
		clientConn.Close()
		p.metrics.mu.Lock()
		p.metrics.ActiveConnections--
		p.metrics.mu.Unlock()

		// Release connection slot back to semaphore
		<-p.connSemaphore
	}()

	// Extract source IP (preserved by transparent proxy)
	remoteIP := extractIP(clientConn.RemoteAddr())
	if remoteIP == nil {
		p.logger.Warn("Failed to extract remote IP", zap.String("remote_addr", clientConn.RemoteAddr().String()))
		return
	}

	// Create connection object
	conn := &Connection{
		ID:             fmt.Sprintf("%s-%d", remoteIP.String(), time.Now().Unix()),
		RemoteAddr:     remoteIP,
		LocalAddr:      clientConn.LocalAddr(),
		ConnectedAt:    time.Now(),
		ClientConn:     clientConn,
		State:          StateConnected,
		LastCommandTime: time.Now(),
		Metrics: &scoring.ConnectionMetrics{
			IP:             remoteIP,
			ConnectedAt:    time.Now(),
			CommandTimings: make([]time.Duration, 0),
		},
	}

	// Track connection
	p.connMu.Lock()
	p.activeConns[conn.ID] = conn
	p.connMu.Unlock()

	defer func() {
		p.connMu.Lock()
		delete(p.activeConns, conn.ID)
		p.connMu.Unlock()
	}()

	p.logger.Info("New connection",
		zap.String("conn_id", conn.ID),
		zap.String("remote_ip", remoteIP.String()))

	// Pre-banner analysis
	if err := p.preBannerAnalysis(conn); err != nil {
		p.logger.Warn("Pre-banner analysis error", zap.Error(err))
		return
	}

	// If pre-banner talk detected, score and potentially drop
	if conn.Metrics.PreBannerTalk {
		p.logger.Warn("Pre-banner talk detected - instant DROP",
			zap.String("conn_id", conn.ID),
			zap.String("remote_ip", remoteIP.String()))

		// Score it (will be 100)
		decision, _ := p.scoringEngine.CalculateScore(conn.Metrics)
		conn.Decision = decision

		// Update metrics and add to blacklist
		p.scoringEngine.UpdateMetrics(conn.Metrics, decision)
		p.nftables.AddToBlacklist(remoteIP, 24*time.Hour)

		p.metrics.mu.Lock()
		p.metrics.DroppedConnections++
		p.metrics.mu.Unlock()

		return // Drop connection
	}

	// Send SMTP banner
	if err := p.sendBanner(conn); err != nil {
		p.logger.Error("Failed to send banner", zap.Error(err))
		return
	}

	// Process SMTP commands and proxy to backend
	p.processSMTPCommands(conn)
}

// preBannerAnalysis watches for pre-banner talking
func (p *TransparentProxy) preBannerAnalysis(conn *Connection) error {
	// Set read deadline for pre-banner period
	conn.ClientConn.SetReadDeadline(time.Now().Add(p.config.PreBannerTimeout))

	// Try to read - if client talks before banner, it's a violation
	buf := make([]byte, 1024)
	n, err := conn.ClientConn.Read(buf)

	// Reset deadline
	conn.ClientConn.SetReadDeadline(time.Time{})

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout = good, client waited for banner
			return nil
		}
		return err
	}

	if n > 0 {
		// Client sent data before banner = pre-banner talk!
		conn.Metrics.PreBannerTalk = true
		p.logger.Warn("Pre-banner talk detected",
			zap.String("conn_id", conn.ID),
			zap.ByteString("data", buf[:n]))
		return nil
	}

	return nil
}

// sendBanner sends the SMTP 220 banner
func (p *TransparentProxy) sendBanner(conn *Connection) error {
	banner := fmt.Sprintf("220 %s ESMTP ADS PreMail\r\n", p.config.ServerName)
	_, err := conn.ClientConn.Write([]byte(banner))
	return err
}

// processSMTPCommands reads and processes SMTP commands
func (p *TransparentProxy) processSMTPCommands(conn *Connection) {
	reader := bufio.NewReader(conn.ClientConn)

	for {
		// Set read timeout
		conn.ClientConn.SetReadDeadline(time.Now().Add(p.config.ReadTimeout))

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				p.logger.Debug("Connection read error",
					zap.String("conn_id", conn.ID),
					zap.Error(err))
			}
			break
		}

		// Track command timing
		now := time.Now()
		if !conn.LastCommandTime.IsZero() {
			timing := now.Sub(conn.LastCommandTime)
			conn.CommandTimings = append(conn.CommandTimings, timing)
			conn.Metrics.CommandTimings = conn.CommandTimings
		}
		conn.LastCommandTime = now

		// Parse command
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}

		p.logger.Debug("SMTP command received",
			zap.String("conn_id", conn.ID),
			zap.String("command", cmd))

		conn.Metrics.SMTPCommandsIssued++

		// Analyze command
		if !p.isValidSMTPCommand(cmd) {
			conn.Metrics.InvalidCommands++
		}

		// Handle QUIT
		if strings.HasPrefix(strings.ToUpper(cmd), "QUIT") {
			p.sendResponse(conn, "221 Bye\r\n")
			break
		}

		// At certain points, calculate score and decide action
		if p.shouldScore(conn) {
			decision, err := p.scoringEngine.CalculateScore(conn.Metrics)
			if err != nil {
				p.logger.Error("Scoring error", zap.Error(err))
				break
			}

			conn.Decision = decision

			// Take action based on score
			action := p.handleScoringDecision(conn, decision)
			if action == scoring.ActionDrop {
				p.logger.Info("Dropping connection",
					zap.String("conn_id", conn.ID),
					zap.Int("score", decision.Score))
				break
			}
		}

		// If we haven't opened backend connection yet and score is acceptable
		if conn.BackendConn == nil && (conn.Decision == nil || conn.Decision.Action == scoring.ActionAllow) {
			if err := p.connectToBackend(conn); err != nil {
				p.logger.Error("Failed to connect to backend", zap.Error(err))
				p.sendResponse(conn, "421 Service temporarily unavailable\r\n")
				break
			}
		}

		// Forward to backend if connected
		if conn.BackendConn != nil {
			if _, err := conn.BackendConn.Write([]byte(line)); err != nil {
				p.logger.Error("Failed to write to backend", zap.Error(err))
				break
			}

			// Read backend response
			backendReader := bufio.NewReader(conn.BackendConn)
			response, err := backendReader.ReadString('\n')
			if err != nil {
				p.logger.Error("Failed to read from backend", zap.Error(err))
				break
			}

			// Send response to client (with potential delay for tarpit)
			if conn.Decision != nil && conn.Decision.Action == scoring.ActionTarpit {
				time.Sleep(30 * time.Second)
			}

			if _, err := conn.ClientConn.Write([]byte(response)); err != nil {
				p.logger.Error("Failed to write to client", zap.Error(err))
				break
			}
		}
	}

	// Connection ended - finalize metrics
	now := time.Now()
	conn.DisconnectedAt = &now
	conn.Metrics.DisconnectedAt = &now
	conn.Metrics.ConnectionDuration = now.Sub(conn.ConnectedAt)

	// Check for quick disconnect
	if conn.Metrics.ConnectionDuration < 2*time.Second {
		conn.Metrics.QuickDisconnect = true
	}

	// Calculate timing variance
	if len(conn.CommandTimings) > 1 {
		conn.Metrics.TimingVariance = calculateVariance(conn.CommandTimings)
	}

	// Final scoring if not done yet
	if conn.Decision == nil {
		decision, _ := p.scoringEngine.CalculateScore(conn.Metrics)
		conn.Decision = decision
	}

	// Update metrics in database
	p.scoringEngine.UpdateMetrics(conn.Metrics, conn.Decision)

	p.logger.Info("Connection closed",
		zap.String("conn_id", conn.ID),
		zap.Duration("duration", conn.Metrics.ConnectionDuration),
		zap.Int("score", conn.Decision.Score),
		zap.String("action", string(conn.Decision.Action)))
}

// connectToBackend establishes connection to backend mail server
func (p *TransparentProxy) connectToBackend(conn *Connection) error {
	// Round-robin backend selection
	p.backendMu.Lock()
	backend := p.backends[p.backendIndex]
	p.backendIndex = (p.backendIndex + 1) % len(p.backends)
	p.backendMu.Unlock()

	backendConn, err := net.DialTimeout("tcp", backend, 10*time.Second)
	if err != nil {
		p.metrics.mu.Lock()
		p.metrics.BackendFailures++
		p.metrics.mu.Unlock()
		return fmt.Errorf("failed to connect to backend %s: %w", backend, err)
	}

	conn.BackendConn = backendConn

	p.metrics.mu.Lock()
	p.metrics.BackendForwards++
	p.metrics.mu.Unlock()

	p.logger.Info("Connected to backend",
		zap.String("conn_id", conn.ID),
		zap.String("backend", backend))

	// Read and discard backend's banner (we already sent ours)
	reader := bufio.NewReader(backendConn)
	_, err = reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read backend banner: %w", err)
	}

	return nil
}

// handleScoringDecision takes action based on scoring decision
func (p *TransparentProxy) handleScoringDecision(conn *Connection, decision *scoring.ScoringDecision) scoring.Action {
	switch decision.Action {
	case scoring.ActionDrop:
		p.nftables.AddToBlacklist(conn.RemoteAddr, 24*time.Hour)
		p.nftables.MarkPacket(conn.RemoteAddr, 0x4)
		p.metrics.mu.Lock()
		p.metrics.DroppedConnections++
		p.metrics.mu.Unlock()

	case scoring.ActionTarpit:
		p.nftables.AddToRatelimit(conn.RemoteAddr, 1*time.Hour)
		p.nftables.MarkPacket(conn.RemoteAddr, 0x3)
		p.metrics.mu.Lock()
		p.metrics.TarpitConnections++
		p.metrics.mu.Unlock()

	case scoring.ActionThrottle:
		p.nftables.AddToRatelimit(conn.RemoteAddr, 30*time.Minute)
		p.nftables.MarkPacket(conn.RemoteAddr, 0x2)
		p.metrics.mu.Lock()
		p.metrics.ThrottleConnections++
		p.metrics.mu.Unlock()

	case scoring.ActionMonitor:
		p.nftables.AddToMonitor(conn.RemoteAddr, 30*time.Minute)
		p.nftables.MarkPacket(conn.RemoteAddr, 0x1)
		p.metrics.mu.Lock()
		p.metrics.MonitorConnections++
		p.metrics.mu.Unlock()

	case scoring.ActionAllow:
		p.metrics.mu.Lock()
		p.metrics.AllowedConnections++
		p.metrics.mu.Unlock()
	}

	return decision.Action
}

// Helper functions

func (p *TransparentProxy) shouldScore(conn *Connection) bool {
	// Score after HELO, or after 5 commands, or after 30 seconds
	return conn.State == StateHelo ||
		conn.Metrics.SMTPCommandsIssued >= 5 ||
		time.Since(conn.ConnectedAt) > 30*time.Second
}

func (p *TransparentProxy) isValidSMTPCommand(cmd string) bool {
	upper := strings.ToUpper(cmd)
	validCommands := []string{
		"HELO", "EHLO", "MAIL FROM", "RCPT TO", "DATA",
		"RSET", "VRFY", "EXPN", "HELP", "NOOP", "QUIT",
		"STARTTLS", "AUTH",
	}

	for _, valid := range validCommands {
		if strings.HasPrefix(upper, valid) {
			return true
		}
	}

	return false
}

func (p *TransparentProxy) sendResponse(conn *Connection, response string) {
	conn.ClientConn.Write([]byte(response))
}

func extractIP(addr net.Addr) net.IP {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP
	}
	return nil
}

func calculateVariance(timings []time.Duration) float64 {
	if len(timings) == 0 {
		return 0
	}

	// Calculate mean
	var sum time.Duration
	for _, t := range timings {
		sum += t
	}
	mean := float64(sum) / float64(len(timings))

	// Calculate variance
	var variance float64
	for _, t := range timings {
		diff := float64(t) - mean
		variance += diff * diff
	}
	variance /= float64(len(timings))

	return variance
}
