package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/version"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	"github.com/afterdarksys/go-emailservice-ads/internal/jmap"
	"github.com/afterdarksys/go-emailservice-ads/internal/metrics"
	"github.com/afterdarksys/go-emailservice-ads/internal/netutil"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/replication"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print release version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Version)
		return
	}

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
	if len(cfg.Platform.Listeners) == 0 {
		portChecker.Check("SMTP", cfg.Server.Addr)
	} else {
		for _, l := range cfg.Platform.Listeners {
			portChecker.Check("SMTP "+l.Role, l.Addr)
		}
	}
	if !cfg.IMAP.Disabled {
		portChecker.Check("IMAP", cfg.IMAP.Addr)
	}
	if cfg.JMAP.Enabled {
		portChecker.Check("JMAP", cfg.JMAP.Addr)
	}
	portChecker.Check("REST API", cfg.API.RESTAddr)
	// gRPC is not implemented; no port is opened.

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
	storagePath := filepath.Join(cfg.Platform.DataDir, "mail-storage")
	store, err := storage.NewMessageStore(storagePath, logger)
	if err != nil {
		logger.Fatal("Failed to initialize message store", zap.Error(err))
	}
	defer store.Close()
	store.SetLimits(storage.Limits{MaxBytes: cfg.Platform.MaxSpoolBytes, MaxMessages: cfg.Platform.MaxSpoolMessages, MinFreeBytes: cfg.Platform.MinFreeBytes})

	// Initialize IMAP adapter and SQLite-backed mailbox store for local delivery
	imapAdapter := storage.NewIMAPAdapter(store)
	mailboxDBPath := filepath.Join(cfg.Platform.DataDir, "mailbox.db")
	imapStore, err := storage.NewMailboxStore(imapAdapter, mailboxDBPath)
	if err != nil {
		logger.Fatal("Failed to initialize mailbox store", zap.Error(err))
	}
	defer imapStore.Close()

	// Initialize outbound DKIM signing — one signer per configured sending
	// domain (empty map disables signing entirely).
	dkimSigners := make(map[string]*security.Signer)
	for _, dc := range cfg.Server.DKIM {
		if !dc.Enabled {
			continue
		}
		if dc.Domain == "" {
			logger.Fatal("DKIM signing entry is enabled but has no domain set")
		}
		signer, err := security.NewSigner(logger, dc.Domain, dc.Selector, dc.PrivateKeyPath)
		if err != nil {
			logger.Fatal("Failed to initialize DKIM signer", zap.String("domain", dc.Domain), zap.Error(err))
		}
		if signer.GetOptions() == nil {
			logger.Fatal("DKIM signing is enabled but no private key was loaded",
				zap.String("domain", dc.Domain), zap.String("private_key_path", dc.PrivateKeyPath))
		}
		dkimSigners[strings.ToLower(dc.Domain)] = signer
	}

	// Initialize queue manager with persistence
	queueManager := smtpd.NewQueueManager(logger, store, imapStore, cfg.Server.Domain, cfg.Server.LocalDomains, dkimSigners)
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
		ConfigPath: cfg.Platform.PolicyPath,
		Logger:     logger,
	}
	policyMgr, err = policy.NewManager(policyConfig)
	if err != nil {
		logger.Warn("Failed to initialize policy manager", zap.Error(err))
		if cfg.Platform.PolicyRequired {
			logger.Fatal("Required policy initialization failed", zap.Error(err))
		}
		// Continue without policies
		policyMgr = nil
	} else {
		if err := policyMgr.UsePersistentFile(filepath.Join(cfg.Platform.DataDir, "policies.yaml")); err != nil {
			logger.Fatal("Persistent policy management failed", zap.Error(err))
		}
		logger.Info("Policy manager initialized")
		queueManager.SetPolicyManager(policyMgr)
	}

	// Create the shared auth validator/user store before the API server so
	// the REST mailbox-management endpoints operate on the same store the
	// IMAP server authenticates against.
	imapValidator := auth.NewValidator(logger)
	imapUserStore := imapValidator.GetUserStore()
	imapUserStore.SetLogger(logger)

	// Persistent user store (optional). Fail closed: a configured database
	// that cannot be opened must stop startup rather than silently running
	// with an empty in-memory user set.
	if cfg.Auth.UserDatabaseURL != "" {
		userRepo, err := auth.NewUserRepository(cfg.Auth.UserDatabaseURL, logger)
		if err != nil {
			logger.Fatal("Failed to open user database", zap.Error(err))
		}
		defer userRepo.Close()
		if err := imapUserStore.SetRepository(userRepo); err != nil {
			logger.Fatal("Failed to load users from database", zap.Error(err))
		}
		if err := imapValidator.LoadDomainEntitlements(); err != nil {
			logger.Fatal("Failed to load domain entitlements", zap.Error(err))
		}
	}

	// Initialize SSO for IMAP if enabled
	if cfg.SSO.Enabled {
		ssoProvider := auth.NewSSOProvider(cfg, logger)
		if ssoProvider != nil {
			imapUserStore.SetSSOProvider(ssoProvider)
		}
	}

	// Load default users. With a persistent store these are bootstrap-only:
	// created when missing, but never overwriting an existing user, so
	// password changes made through the admin API survive restarts.
	for _, userCfg := range cfg.Auth.DefaultUsers {
		if cfg.Auth.UserDatabaseURL != "" {
			if _, exists := imapUserStore.GetUser(userCfg.Username); exists {
				continue
			}
		}
		if err := imapUserStore.AddUser(userCfg.Username, userCfg.Password, userCfg.Email); err != nil {
			logger.Error("Failed to add IMAP user", zap.String("username", userCfg.Username), zap.Error(err))
		}
	}

	if err := queueManager.ConfigurePlatform(cfg, imapUserStore); err != nil {
		logger.Fatal("Platform configuration failed", zap.Error(err))
	}
	retryScheduler.Start()
	// Start API Servers with full dependencies
	apiServer := api.NewServer(cfg, logger, store, queueManager, replicator, metricsCollector, policyMgr, imapUserStore)
	if err := apiServer.Start(); err != nil {
		logger.Fatal("API startup failed", zap.Error(err))
	}

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
	var smtpServers []*smtpd.Server
	listenerConfigs := []*config.Config{cfg}
	if len(cfg.Platform.Listeners) > 0 {
		listenerConfigs = nil
		for _, listener := range cfg.Platform.Listeners {
			copyCfg := *cfg
			copyCfg.Server.Addr = listener.Addr
			copyCfg.Server.Role = listener.Role
			copyCfg.Server.TLS = listener.TLS
			copyCfg.Server.TrustedNetworks = listener.TrustedNetworks
			copyCfg.Server.ProxyProtocol = listener.ProxyProtocol
			copyCfg.Server.RequireAuth = listener.Role == "submission"
			copyCfg.Server.RequireTLS = listener.Role != "perimeter"
			copyCfg.Server.AllowInsecureAuth = false
			listenerConfigs = append(listenerConfigs, &copyCfg)
		}
	}
	for _, listenerCfg := range listenerConfigs {
		server := smtpd.NewServerWithValidator(listenerCfg, logger, queueManager, policyMgr, imapValidator)
		smtpServers = append(smtpServers, server)
		go func() {
			if err := server.ListenAndServe(); err != nil {
				logger.Fatal("SMTP listener stopped", zap.Error(err))
			}
		}()
	}

	// Start IMAP Server (if enabled) — shares imapValidator with the REST API.
	// Reuse the mailbox store already created for the queue manager
	var imapServer *imap.Server
	if !cfg.IMAP.Disabled {
		imapServer = imap.NewServer(logger, imapStore, cfg, imapValidator)
		go func() {
			if err := imapServer.Start(); err != nil {
				logger.Fatal("IMAP server failed", zap.Error(err))
			}
		}()

	}
	// JMAP shares the authenticated mailbox store with IMAP. It is disabled by
	// default so operators can place it behind an HTTPS reverse proxy explicitly.
	var jmapServer *jmap.JMAPServer
	if cfg.JMAP.Enabled {
		jmapServer = jmap.NewJMAPServer(logger, cfg, imapValidator, imapAdapter)
		if err := jmapServer.Start(cfg.JMAP.Addr); err != nil {
			logger.Fatal("JMAP server failed", zap.Error(err))
		}
	}

	// Graceful Shutdown Handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Received shutdown signal, shutting down systems...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, server := range smtpServers {
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("SMTP shutdown", zap.Error(err))
		}
	}
	if imapServer != nil {
		if err := imapServer.Shutdown(ctx); err != nil {
			logger.Error("Error during IMAP server shutdown", zap.Error(err))
		}
	}
	if jmapServer != nil {
		if err := jmapServer.Shutdown(ctx); err != nil {
			logger.Error("Error during JMAP server shutdown", zap.Error(err))
		}
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
      password: "REPLACE_ME_USE_A_STRONG_PASSWORD"
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
