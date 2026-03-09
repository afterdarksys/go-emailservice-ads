package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtp"
	"github.com/afterdarksys/go-emailservice-ads/internal/api"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/elasticsearch"
	"github.com/afterdarksys/go-emailservice-ads/internal/imap"
	"github.com/afterdarksys/go-emailservice-ads/internal/metrics"
	"github.com/afterdarksys/go-emailservice-ads/internal/netutil"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/replication"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Setup fallback logger in case config isn't loaded yet
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Create default config if it doesn't exist
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		logger.Warn("Config file not found, creating default config", zap.String("path", *configPath))
		if err := createDefaultConfig(*configPath); err != nil {
			logger.Fatal("Failed to create default config", zap.Error(err))
		}
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	// Re-configure logger based on config
	level, err := zapcore.ParseLevel(cfg.Logging.Level)
	if err == nil {
		core := zapcore.NewCore(
			zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
			os.Stdout,
			level,
		)
		logger = zap.New(core)
		logger.Info("Reconfigured logger", zap.String("level", level.String()))
	}

	logger.Info("Starting go-emailservice-ads module")

	// Check port availability before starting services
	portChecker := netutil.NewPortChecker()
	portChecker.Check("SMTP", cfg.Server.Addr)
	portChecker.Check("IMAP", cfg.IMAP.Addr)
	portChecker.Check("REST API", cfg.API.RESTAddr)
	portChecker.Check("gRPC API", cfg.API.GRPCAddr)

	if !portChecker.AllAvailable() {
		logger.Error("Port conflict detected:\n" + portChecker.FormatReport())
		failures := portChecker.GetFailures()
		if len(failures) > 0 {
			logger.Fatal("Cannot start server due to port conflicts. Please resolve the conflicts and try again.")
		}
	}
	logger.Info("Port availability check passed", zap.String("report", portChecker.FormatReport()))

	// Initialize metrics collector
	metricsCollector := metrics.NewMetrics(logger)

	// Initialize persistent storage
	storagePath := filepath.Join(".", "data", "mail-storage")
	store, err := storage.NewMessageStore(storagePath, logger)
	if err != nil {
		logger.Fatal("Failed to initialize message store", zap.Error(err))
	}
	defer store.Close()

	// Initialize queue manager with persistence
	queueManager := smtpd.NewQueueManager(logger, store, cfg.Server.Domain, cfg.Server.LocalDomains)
	defer queueManager.Shutdown()

	// Initialize Elasticsearch integration (optional)
	if cfg.Elasticsearch.Enabled {
		logger.Info("Initializing Elasticsearch integration")

		esClient, err := elasticsearch.NewClient(cfg, logger)
		if err != nil {
			logger.Error("Failed to initialize Elasticsearch client",
				zap.Error(err))
			logger.Warn("Continuing without Elasticsearch integration")
		} else {
			// Create index template and ILM policy
			ctx := context.Background()
			if err := esClient.CreateIndexTemplate(ctx); err != nil {
				logger.Error("Failed to create index template", zap.Error(err))
			}
			if err := esClient.CreateILMPolicy(ctx); err != nil {
				logger.Error("Failed to create ILM policy", zap.Error(err))
			}
			if err := esClient.EnsureIndex(ctx); err != nil {
				logger.Error("Failed to ensure index exists", zap.Error(err))
			}

			// Create indexer
			esIndexer, err := elasticsearch.NewIndexer(esClient, logger)
			if err != nil {
				logger.Error("Failed to initialize Elasticsearch indexer",
					zap.Error(err))
			} else {
				// Attach indexer to queue manager
				queueManager.SetElasticsearchIndexer(esIndexer)
				logger.Info("Elasticsearch integration enabled",
					zap.String("index_prefix", cfg.Elasticsearch.IndexPrefix),
					zap.Float64("sampling_rate", cfg.Elasticsearch.SamplingRate))
			}
		}
	}

	// Initialize retry scheduler
	retryPolicy := smtpd.DefaultRetryPolicy()
	retryScheduler := smtpd.NewRetryScheduler(store, queueManager, retryPolicy, logger)
	retryScheduler.Start()
	defer retryScheduler.Shutdown()

	// Initialize replication (optional, configured in config)
	var replicator *replication.Replicator
	// TODO: Add replication config to config.yaml
	// For now, initialize as standalone (no replication)
	// replicator, err = replication.NewReplicator(store, replication.ModePrimary, ":9090", []string{}, logger)
	// if err != nil {
	// 	logger.Fatal("Failed to initialize replicator", zap.Error(err))
	// }
	// defer replicator.Shutdown()

	// Initialize policy manager (shared between SMTP and API servers)
	var policyMgr *policy.Manager
	policyConfig := &policy.ManagerConfig{
		ConfigPath: "policies.yaml",
		Logger:     logger,
	}
	policyMgr, err = policy.NewManager(policyConfig)
	if err != nil {
		logger.Warn("Failed to initialize policy manager", zap.Error(err))
		// Continue without policies
		policyMgr = nil
	} else {
		logger.Info("Policy manager initialized")
	}

	// Start API Servers with full dependencies
	apiServer := api.NewServer(cfg, logger, store, queueManager, replicator, metricsCollector, policyMgr)
	apiServer.Start()

	// Start AfterSMTP Bridge Service (if enabled)
	var amtpSrv *aftersmtp.Service
	if cfg.AfterSMTP.Enabled {
		amSrv, err := aftersmtp.NewService(cfg, logger, queueManager)
		if err != nil {
			logger.Fatal("Failed to initialize AfterSMTP Bridge", zap.Error(err))
		}
		amtpSrv = amSrv
		amtpSrv.Start()
	}

	// Start ESMTP Server with queue manager and policy manager
	smtpServer := smtpd.NewServer(cfg, logger, queueManager, policyMgr)
	go func() {
		if err := smtpServer.ListenAndServe(); err != nil {
			logger.Fatal("SMTP server failed", zap.Error(err))
		}
	}()

	// Start IMAP Server (if enabled)
	// Create a validator for IMAP authentication (could share with SMTP in production)
	imapValidator := auth.NewValidator(logger)
	imapUserStore := imapValidator.GetUserStore()
	imapUserStore.SetLogger(logger)

	// Initialize SSO for IMAP if enabled
	if cfg.SSO.Enabled {
		ssoProvider := auth.NewSSOProvider(cfg, logger)
		if ssoProvider != nil {
			imapUserStore.SetSSOProvider(ssoProvider)
		}
	}

	// Load default users for IMAP as well
	for _, userCfg := range cfg.Auth.DefaultUsers {
		if err := imapUserStore.AddUser(userCfg.Username, userCfg.Password, userCfg.Email); err != nil {
			logger.Error("Failed to add IMAP user", zap.String("username", userCfg.Username), zap.Error(err))
		}
	}

	// Create IMAP adapter for the message store
	imapStore := storage.NewIMAPAdapter(store)
	imapServer := imap.NewServer(logger, imapStore, cfg, imapValidator)
	go func() {
		if err := imapServer.Start(); err != nil {
			logger.Fatal("IMAP server failed", zap.Error(err))
		}
	}()

	// Graceful Shutdown Handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Received shutdown signal, shutting down systems...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := smtpServer.Shutdown(ctx); err != nil {
		logger.Error("Error during SMTP server shutdown", zap.Error(err))
	}
	if err := imapServer.Shutdown(ctx); err != nil {
		logger.Error("Error during IMAP server shutdown", zap.Error(err))
	}
	if err := apiServer.Shutdown(ctx); err != nil {
		logger.Error("Error during API server shutdown", zap.Error(err))
	}
	if amtpSrv != nil {
		amtpSrv.Shutdown()
	}

	logger.Info("Shutdown complete.")
}

func createDefaultConfig(path string) error {
	content := []byte(`server:
  addr: ":2525"
  domain: "localhost.local"
  max_message_bytes: 10485760
  max_recipients: 50
  allow_insecure_auth: false   # SECURITY: Require TLS for AUTH
  require_auth: true            # SECURITY: Require authentication
  require_tls: true             # SECURITY: Require STARTTLS
  mode: "test"
  tls:
    cert: "./data/certs/server.crt"
    key: "./data/certs/server.key"
imap:
  addr: ":1143"
  require_tls: true
  tls:
    cert: "./data/certs/server.crt"
    key: "./data/certs/server.key"
api:
  rest_addr: ":8080"
  grpc_addr: ":50051"
auth:
  default_users:
    - username: "testuser"
      password: "testpass123"
      email: "testuser@localhost.local"
aftersmtp:
  enabled: false
  ledger_url: "ws://127.0.0.1:9944"
  quic_addr: ":4434"
  grpc_addr: ":4433"
  fallback_db: "fallback_ledger.db"
logging:
  level: "debug"
`)
	return os.WriteFile(path, content, 0644)
}
