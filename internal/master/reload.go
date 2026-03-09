package master

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// hotReloadWatcher watches for config file changes and triggers reload
func (c *Controller) hotReloadWatcher() {
	defer c.wg.Done()

	configPath := "master.yaml" // TODO: Make this configurable
	interval, _ := time.ParseDuration(c.config.HotReload.CheckInterval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastHash := c.computeFileHash(configPath)
	c.logger.Info("Hot reload watcher started",
		zap.String("config_path", configPath),
		zap.Duration("interval", interval))

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Hot reload watcher stopped")
			return

		case <-ticker.C:
			currentHash := c.computeFileHash(configPath)
			if currentHash == "" {
				continue // File error, skip this check
			}

			if currentHash != lastHash {
				c.logger.Info("Configuration file changed, reloading",
					zap.String("config_path", configPath))

				if err := c.reloadFromFile(configPath); err != nil {
					c.logger.Error("Failed to reload configuration",
						zap.Error(err))
					c.updateStats(func(s *ControllerStats) {
						s.Errors++
					})
					continue
				}

				lastHash = currentHash
			}
		}
	}
}

// reloadFromFile loads configuration from file and applies it
func (c *Controller) reloadFromFile(path string) error {
	// Backup current config if enabled
	if c.config.HotReload.BackupOnChange {
		if err := c.backupConfig(path); err != nil {
			c.logger.Warn("Failed to backup config", zap.Error(err))
		}
	}

	// Load new configuration
	newConfig, err := LoadMasterConfig(path)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate if enabled
	if c.config.HotReload.ValidateBeforeApply {
		if err := newConfig.Validate(); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	// Apply the new configuration
	if err := c.Reload(newConfig); err != nil {
		return fmt.Errorf("failed to apply config: %w", err)
	}

	return nil
}

// backupConfig creates a timestamped backup of the config file
func (c *Controller) backupConfig(path string) error {
	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(
		filepath.Dir(path),
		fmt.Sprintf("%s.backup.%s", filepath.Base(path), timestamp),
	)

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	c.logger.Info("Configuration backed up",
		zap.String("backup_path", backupPath))

	return nil
}

// computeFileHash computes SHA256 hash of file content
func (c *Controller) computeFileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		c.logger.Debug("Failed to open file for hashing",
			zap.String("path", path),
			zap.Error(err))
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		c.logger.Debug("Failed to compute hash",
			zap.String("path", path),
			zap.Error(err))
		return ""
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// ValidateConfigFile validates a configuration file without loading it
func ValidateConfigFile(path string) error {
	config, err := LoadMasterConfig(path)
	if err != nil {
		return err
	}

	return config.Validate()
}

// RestoreFromBackup restores configuration from a backup file
func (c *Controller) RestoreFromBackup(backupPath string) error {
	c.logger.Info("Restoring configuration from backup",
		zap.String("backup_path", backupPath))

	// Validate backup file
	if err := ValidateConfigFile(backupPath); err != nil {
		return fmt.Errorf("backup file validation failed: %w", err)
	}

	// Load and apply
	return c.reloadFromFile(backupPath)
}

// ListBackups returns available config backups
func ListBackups(configDir string) ([]string, error) {
	pattern := filepath.Join(configDir, "master.yaml.backup.*")
	backups, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	return backups, nil
}
