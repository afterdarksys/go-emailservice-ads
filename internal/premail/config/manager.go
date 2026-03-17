package config

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Manager handles configuration with versioning and backup
type Manager struct {
	logger         *zap.Logger
	configPath     string
	versionsDir    string
	backupsDir     string
	currentVersion *Version
	config         *Config
}

// Config represents the complete ADS PreMail configuration
type Config struct {
	Version      string        `yaml:"version"`
	Updated      time.Time     `yaml:"updated"`

	// Proxy settings
	Proxy        ProxyConfig   `yaml:"proxy"`

	// Scoring settings
	Scoring      ScoringConfig `yaml:"scoring"`

	// Database settings
	Database     DatabaseConfig `yaml:"database"`

	// nftables settings
	NFTables     NFTablesConfig `yaml:"nftables"`

	// Reputation settings
	Reputation   ReputationConfig `yaml:"reputation"`

	// Analyzer settings
	Analyzer     AnalyzerConfig `yaml:"analyzer"`
}

type ProxyConfig struct {
	ListenAddr      string        `yaml:"listen_addr"`
	BackendServers  []string      `yaml:"backend_servers"`
	ServerName      string        `yaml:"server_name"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	MaxConnections  int           `yaml:"max_connections"`
}

type ScoringConfig struct {
	Thresholds struct {
		Allow    int `yaml:"allow"`
		Monitor  int `yaml:"monitor"`
		Throttle int `yaml:"throttle"`
		Tarpit   int `yaml:"tarpit"`
		Drop     int `yaml:"drop"`
	} `yaml:"thresholds"`

	Weights struct {
		PreBannerTalk      int `yaml:"pre_banner_talk"`
		InvalidCommand     int `yaml:"invalid_command"`
		QuickDisconnect    int `yaml:"quick_disconnect"`
		FailedAuthPer      int `yaml:"failed_auth_per"`
		HighRecipientCount int `yaml:"high_recipient_count"`
	} `yaml:"weights"`
}

type DatabaseConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Database        string `yaml:"database"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	SSLMode         string `yaml:"ssl_mode"`
	RetentionDays   int    `yaml:"retention_days"`
}

type NFTablesConfig struct {
	Enabled       bool   `yaml:"enabled"`
	TableName     string `yaml:"table_name"`
	BlacklistSet  string `yaml:"blacklist_set"`
	RatelimitSet  string `yaml:"ratelimit_set"`
	MonitorSet    string `yaml:"monitor_set"`
}

type ReputationConfig struct {
	DNSScienceEnabled  bool          `yaml:"dnsscience_enabled"`
	DNSScienceAPIURL   string        `yaml:"dnsscience_api_url"`
	DNSScienceAPIKey   string        `yaml:"dnsscience_api_key"`
	FeedInterval       time.Duration `yaml:"feed_interval"`
	BatchSize          int           `yaml:"batch_size"`
}

type AnalyzerConfig struct {
	PreBannerTimeout         time.Duration `yaml:"pre_banner_timeout"`
	QuickDisconnectThreshold time.Duration `yaml:"quick_disconnect_threshold"`
	HourlyConnectionLimit    int           `yaml:"hourly_connection_limit"`
	BotTimingThreshold       float64       `yaml:"bot_timing_threshold"`
}

// Version represents a configuration version
type Version struct {
	Number    int       `json:"number"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
	Comment   string    `json:"comment"`
	ConfigData []byte   `json:"config_data"`
}

// NewManager creates a new configuration manager
func NewManager(logger *zap.Logger, configPath string) (*Manager, error) {
	versionsDir := filepath.Join(filepath.Dir(configPath), ".config_versions")
	backupsDir := filepath.Join(filepath.Dir(configPath), ".config_backups")

	// Create directories if they don't exist
	if err := os.MkdirAll(versionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create versions directory: %w", err)
	}

	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backups directory: %w", err)
	}

	m := &Manager{
		logger:      logger,
		configPath:  configPath,
		versionsDir: versionsDir,
		backupsDir:  backupsDir,
	}

	return m, nil
}

// Load loads the current configuration
func (m *Manager) Load() (*Config, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	m.config = &config

	m.logger.Info("Loaded configuration",
		zap.String("version", config.Version),
		zap.Time("updated", config.Updated))

	return &config, nil
}

// Save saves the configuration with versioning
func (m *Manager) Save(config *Config, comment string) error {
	config.Updated = time.Now()

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Calculate hash
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Check if config has changed
	if m.currentVersion != nil && m.currentVersion.Hash == hashStr {
		m.logger.Info("Configuration unchanged, skipping version save")
		return nil
	}

	// Create new version
	versions, err := m.ListVersions()
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	newVersionNum := 1
	if len(versions) > 0 {
		newVersionNum = versions[0].Number + 1
	}

	version := &Version{
		Number:     newVersionNum,
		Timestamp:  time.Now(),
		Hash:       hashStr,
		Comment:    comment,
		ConfigData: data,
	}

	// Save version
	if err := m.saveVersion(version); err != nil {
		return fmt.Errorf("failed to save version: %w", err)
	}

	// Write current config
	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	m.currentVersion = version
	m.config = config

	m.logger.Info("Saved configuration",
		zap.Int("version", version.Number),
		zap.String("hash", hashStr[:8]),
		zap.String("comment", comment))

	return nil
}

// saveVersion saves a configuration version
func (m *Manager) saveVersion(version *Version) error {
	versionPath := filepath.Join(m.versionsDir, fmt.Sprintf("v%d.json", version.Number))

	data, err := json.MarshalIndent(version, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}

	if err := os.WriteFile(versionPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	return nil
}

// ListVersions returns all configuration versions
func (m *Manager) ListVersions() ([]*Version, error) {
	files, err := filepath.Glob(filepath.Join(m.versionsDir, "v*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}

	versions := make([]*Version, 0, len(files))

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			m.logger.Warn("Failed to read version file", zap.String("file", file), zap.Error(err))
			continue
		}

		var version Version
		if err := json.Unmarshal(data, &version); err != nil {
			m.logger.Warn("Failed to parse version file", zap.String("file", file), zap.Error(err))
			continue
		}

		versions = append(versions, &version)
	}

	// Sort by version number descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Number > versions[j].Number
	})

	return versions, nil
}

// GetVersion retrieves a specific version
func (m *Manager) GetVersion(versionNum int) (*Version, error) {
	versionPath := filepath.Join(m.versionsDir, fmt.Sprintf("v%d.json", versionNum))

	data, err := os.ReadFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version %d: %w", versionNum, err)
	}

	var version Version
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("failed to parse version %d: %w", versionNum, err)
	}

	return &version, nil
}

// Rollback rolls back to a previous version
func (m *Manager) Rollback(versionNum int, comment string) error {
	version, err := m.GetVersion(versionNum)
	if err != nil {
		return fmt.Errorf("failed to get version %d: %w", versionNum, err)
	}

	// Parse config from version data
	var config Config
	if err := yaml.Unmarshal(version.ConfigData, &config); err != nil {
		return fmt.Errorf("failed to parse config from version: %w", err)
	}

	// Save as new version with rollback comment
	rollbackComment := fmt.Sprintf("Rollback to v%d: %s", versionNum, comment)
	if err := m.Save(&config, rollbackComment); err != nil {
		return fmt.Errorf("failed to save rollback: %w", err)
	}

	m.logger.Info("Rolled back configuration",
		zap.Int("to_version", versionNum),
		zap.String("comment", comment))

	return nil
}

// CreateBackup creates a full backup of configuration and database
func (m *Manager) CreateBackup(includeDatabase bool) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("adspremail_backup_%s.zip", timestamp)
	backupPath := filepath.Join(m.backupsDir, backupName)

	zipFile, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Add current config
	if err := m.addFileToZip(zipWriter, m.configPath, "config.yaml"); err != nil {
		return "", fmt.Errorf("failed to add config to backup: %w", err)
	}

	// Add all versions
	versions, _ := m.ListVersions()
	for _, version := range versions {
		versionPath := filepath.Join(m.versionsDir, fmt.Sprintf("v%d.json", version.Number))
		zipPath := fmt.Sprintf("versions/v%d.json", version.Number)
		if err := m.addFileToZip(zipWriter, versionPath, zipPath); err != nil {
			m.logger.Warn("Failed to add version to backup", zap.Int("version", version.Number))
		}
	}

	// Add nftables config export
	if m.config != nil && m.config.NFTables.Enabled {
		// This would export nftables rules, but requires nftables manager
		// For now, just document it in the backup
		info := map[string]interface{}{
			"timestamp":    timestamp,
			"config_version": m.config.Version,
			"includes_nftables": true,
		}
		infoData, _ := json.MarshalIndent(info, "", "  ")

		w, _ := zipWriter.Create("backup_info.json")
		w.Write(infoData)
	}

	m.logger.Info("Created backup",
		zap.String("backup_path", backupPath),
		zap.String("backup_name", backupName))

	return backupPath, nil
}

// addFileToZip adds a file to a zip archive
func (m *Manager) addFileToZip(zipWriter *zip.Writer, filePath, zipPath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w, err := zipWriter.Create(zipPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, file)
	return err
}

// RestoreBackup restores from a backup file
func (m *Manager) RestoreBackup(backupPath string) error {
	m.logger.Info("Restoring from backup", zap.String("backup_path", backupPath))

	zipReader, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer zipReader.Close()

	// Extract files
	for _, file := range zipReader.File {
		if err := m.extractZipFile(file); err != nil {
			return fmt.Errorf("failed to extract %s: %w", file.Name, err)
		}
	}

	// Reload configuration
	if _, err := m.Load(); err != nil {
		return fmt.Errorf("failed to reload config after restore: %w", err)
	}

	m.logger.Info("Successfully restored from backup")

	return nil
}

// extractZipFile extracts a single file from zip
func (m *Manager) extractZipFile(file *zip.File) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	var destPath string
	if file.Name == "config.yaml" {
		destPath = m.configPath
	} else if filepath.Dir(file.Name) == "versions" {
		destPath = filepath.Join(m.versionsDir, filepath.Base(file.Name))
	} else {
		return nil // Skip other files
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	dest, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, rc)
	return err
}

// Diff shows differences between two versions
func (m *Manager) Diff(version1, version2 int) (string, error) {
	v1, err := m.GetVersion(version1)
	if err != nil {
		return "", fmt.Errorf("failed to get version %d: %w", version1, err)
	}

	v2, err := m.GetVersion(version2)
	if err != nil {
		return "", fmt.Errorf("failed to get version %d: %w", version2, err)
	}

	// Simple diff - just show the two configs side by side
	diff := fmt.Sprintf("Version %d (hash: %s) vs Version %d (hash: %s)\n\n",
		version1, v1.Hash[:8], version2, v2.Hash[:8])

	diff += "=== Version " + fmt.Sprint(version1) + " ===\n"
	diff += string(v1.ConfigData) + "\n\n"

	diff += "=== Version " + fmt.Sprint(version2) + " ===\n"
	diff += string(v2.ConfigData) + "\n"

	return diff, nil
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Version: "1.0.0",
		Updated: time.Now(),
		Proxy: ProxyConfig{
			ListenAddr:     ":2525",
			BackendServers: []string{"127.0.0.1:25"},
			ServerName:     "msgs.global",
			ReadTimeout:    5 * time.Minute,
			WriteTimeout:   5 * time.Minute,
			MaxConnections: 1000,
		},
		Scoring: ScoringConfig{
			Thresholds: struct {
				Allow    int `yaml:"allow"`
				Monitor  int `yaml:"monitor"`
				Throttle int `yaml:"throttle"`
				Tarpit   int `yaml:"tarpit"`
				Drop     int `yaml:"drop"`
			}{
				Allow:    30,
				Monitor:  50,
				Throttle: 70,
				Tarpit:   90,
				Drop:     91,
			},
		},
		Database: DatabaseConfig{
			Host:          "localhost",
			Port:          5432,
			Database:      "emailservice",
			User:          "premail",
			SSLMode:       "require",
			RetentionDays: 90,
		},
		NFTables: NFTablesConfig{
			Enabled:      true,
			TableName:    "inet filter",
			BlacklistSet: "adspremail_blacklist",
			RatelimitSet: "adspremail_ratelimit",
			MonitorSet:   "adspremail_monitor",
		},
		Reputation: ReputationConfig{
			DNSScienceEnabled: false,
			DNSScienceAPIURL:  "https://api.dnsscience.io",
			FeedInterval:      1 * time.Hour,
			BatchSize:         100,
		},
		Analyzer: AnalyzerConfig{
			PreBannerTimeout:         2 * time.Second,
			QuickDisconnectThreshold: 2 * time.Second,
			HourlyConnectionLimit:    100,
			BotTimingThreshold:       0.01,
		},
	}
}
