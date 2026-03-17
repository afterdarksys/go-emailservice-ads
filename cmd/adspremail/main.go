package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/premail/analyzer"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/nftables"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/proxy"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/repository"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/reputation"
	"github.com/afterdarksys/go-emailservice-ads/internal/premail/scoring"
)

const version = "1.0.0"

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	initConfig := flag.Bool("init", false, "Initialize default configuration")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ADS PreMail v%s\n", version)
		fmt.Println("Transparent SMTP Protection Layer")
		fmt.Println("Inspired by Symantec Turntides 8160")
		os.Exit(0)
	}

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("ADS PreMail starting",
		zap.String("version", version))

	// Initialize config manager
	configMgr, err := config.NewManager(logger, *configPath)
	if err != nil {
		logger.Fatal("Failed to create config manager", zap.Error(err))
	}

	// Handle init command
	if *initConfig {
		defaultCfg := config.DefaultConfig()
		if err := configMgr.Save(defaultCfg, "Initial configuration"); err != nil {
			logger.Fatal("Failed to save default config", zap.Error(err))
		}
		logger.Info("Default configuration created", zap.String("path", *configPath))
		os.Exit(0)
	}

	// Load configuration
	cfg, err := configMgr.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Initialize database repository
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Database,
		cfg.Database.SSLMode,
	)

	repo, err := repository.NewPostgresRepository(connStr, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer repo.Close()

	// Initialize scoring engine
	scoringEngine := scoring.NewEngine(logger, repo)

	// Set custom thresholds if configured
	scoringEngine.SetThresholds(scoring.Thresholds{
		Allow:    cfg.Scoring.Thresholds.Allow,
		Monitor:  cfg.Scoring.Thresholds.Monitor,
		Throttle: cfg.Scoring.Thresholds.Throttle,
		Tarpit:   cfg.Scoring.Thresholds.Tarpit,
		Drop:     cfg.Scoring.Thresholds.Drop,
	})

	// Initialize analyzer
	analyzerCfg := &analyzer.Config{
		PreBannerTimeout:         cfg.Analyzer.PreBannerTimeout,
		QuickDisconnectThreshold: cfg.Analyzer.QuickDisconnectThreshold,
		HourlyConnectionLimit:    cfg.Analyzer.HourlyConnectionLimit,
		BotTimingThreshold:       cfg.Analyzer.BotTimingThreshold,
	}
	connAnalyzer := analyzer.NewAnalyzer(logger, analyzerCfg)

	// Initialize nftables manager
	nftablesCfg := &nftables.Config{
		Enabled:      cfg.NFTables.Enabled,
		TableName:    cfg.NFTables.TableName,
		BlacklistSet: cfg.NFTables.BlacklistSet,
		RatelimitSet: cfg.NFTables.RatelimitSet,
		MonitorSet:   cfg.NFTables.MonitorSet,
	}
	nftablesMgr := nftables.NewManager(logger, nftablesCfg)

	if err := nftablesMgr.Initialize(); err != nil {
		logger.Fatal("Failed to initialize nftables", zap.Error(err))
	}

	// Initialize reputation feed
	reputationCfg := &reputation.Config{
		Enabled:      cfg.Reputation.DNSScienceEnabled,
		APIURL:       cfg.Reputation.DNSScienceAPIURL,
		APIKey:       cfg.Reputation.DNSScienceAPIKey,
		FeedInterval: cfg.Reputation.FeedInterval,
		BatchSize:    cfg.Reputation.BatchSize,
	}
	reputationFeed := reputation.NewDNSScienceFeed(logger, reputationCfg, repo)
	reputationFeed.Start()
	defer reputationFeed.Stop()

	// Initialize transparent proxy
	proxyCfg := &proxy.Config{
		ListenAddr:       cfg.Proxy.ListenAddr,
		BackendServers:   cfg.Proxy.BackendServers,
		ServerName:       cfg.Proxy.ServerName,
		ReadTimeout:      cfg.Proxy.ReadTimeout,
		WriteTimeout:     cfg.Proxy.WriteTimeout,
		MaxConnections:   cfg.Proxy.MaxConnections,
		PreBannerTimeout: cfg.Analyzer.PreBannerTimeout,
	}

	transparentProxy := proxy.NewTransparentProxy(
		proxyCfg,
		logger,
		connAnalyzer,
		scoringEngine,
		nftablesMgr,
	)

	// Start proxy
	if err := transparentProxy.Start(); err != nil {
		logger.Fatal("Failed to start transparent proxy", zap.Error(err))
	}

	logger.Info("ADS PreMail started successfully",
		zap.String("listen_addr", cfg.Proxy.ListenAddr),
		zap.Strings("backends", cfg.Proxy.BackendServers),
		zap.Bool("nftables_enabled", cfg.NFTables.Enabled),
		zap.Bool("reputation_feed_enabled", cfg.Reputation.DNSScienceEnabled))

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh

	logger.Info("Shutting down ADS PreMail...")

	// Cleanup nftables if configured
	// Note: We might want to keep rules active even after shutdown
	// Uncomment if you want to remove rules on shutdown:
	// if cfg.NFTables.Enabled {
	//     nftablesMgr.Cleanup()
	// }

	logger.Info("ADS PreMail stopped")
}
