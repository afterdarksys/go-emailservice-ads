package master

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Controller manages all services defined in master.yaml
type Controller struct {
	config    *MasterConfig
	logger    *zap.Logger
	services  map[string]*ServiceRunner
	listeners map[string]net.Listener
	mu        sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	startTime time.Time
	stats     *ControllerStats
	statsMu   sync.RWMutex
}

// ControllerStats tracks controller metrics
type ControllerStats struct {
	ServicesRunning   int
	TotalConnections  int64
	ActiveConnections int64
	ConfigReloads     int64
	LastReload        time.Time
	Errors            int64
}

// NewController creates a new master controller
func NewController(config *MasterConfig, logger *zap.Logger) *Controller {
	ctx, cancel := context.WithCancel(context.Background())

	return &Controller{
		config:    config,
		logger:    logger,
		services:  make(map[string]*ServiceRunner),
		listeners: make(map[string]net.Listener),
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
		stats:     &ControllerStats{},
	}
}

// Start initializes and starts all enabled services
func (c *Controller) Start() error {
	c.logger.Info("Starting master controller",
		zap.String("version", c.config.Version),
		zap.Int("total_services", len(c.config.Services)))

	c.mu.Lock()
	defer c.mu.Unlock()

	// Start each enabled service
	for name, svc := range c.config.GetEnabledServices() {
		if err := c.startService(name, svc); err != nil {
			c.logger.Error("Failed to start service",
				zap.String("service", name),
				zap.Error(err))
			return fmt.Errorf("failed to start service %s: %w", name, err)
		}
	}

	// Start hot reload watcher if enabled
	if c.config.HotReload.Enabled {
		c.wg.Add(1)
		go c.hotReloadWatcher()
	}

	c.updateStats(func(s *ControllerStats) {
		s.ServicesRunning = len(c.services)
	})

	c.logger.Info("Master controller started successfully",
		zap.Int("services_running", len(c.services)))

	return nil
}

// startService starts a single service
func (c *Controller) startService(name string, svc *Service) error {
	c.logger.Info("Starting service",
		zap.String("name", name),
		zap.String("type", svc.Type),
		zap.String("listen", svc.Listen),
		zap.Int("workers", svc.Workers))

	// Create listener
	listener, err := net.Listen("tcp", svc.Listen)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Create service runner
	runner := &ServiceRunner{
		name:     name,
		service:  svc,
		logger:   c.logger.With(zap.String("service", name)),
		listener: listener,
		ctx:      c.ctx,
	}

	// Store runner and listener
	c.services[name] = runner
	c.listeners[name] = listener

	// Start accepting connections
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := runner.Run(); err != nil {
			c.logger.Error("Service runner error",
				zap.String("service", name),
				zap.Error(err))
			c.updateStats(func(s *ControllerStats) {
				s.Errors++
			})
		}
	}()

	return nil
}

// stopService gracefully stops a service
func (c *Controller) stopService(name string) error {
	runner, exists := c.services[name]
	if !exists {
		return fmt.Errorf("service not found: %s", name)
	}

	c.logger.Info("Stopping service", zap.String("name", name))

	// Close listener to stop accepting new connections
	if listener, ok := c.listeners[name]; ok {
		if err := listener.Close(); err != nil {
			c.logger.Warn("Error closing listener",
				zap.String("service", name),
				zap.Error(err))
		}
		delete(c.listeners, name)
	}

	// Stop the runner
	if err := runner.Stop(); err != nil {
		c.logger.Error("Error stopping service",
			zap.String("service", name),
			zap.Error(err))
		return err
	}

	delete(c.services, name)

	c.logger.Info("Service stopped", zap.String("name", name))
	return nil
}

// Shutdown gracefully stops all services
func (c *Controller) Shutdown(ctx context.Context) error {
	c.logger.Info("Shutting down master controller")

	c.cancel() // Cancel global context

	c.mu.Lock()
	defer c.mu.Unlock()

	// Stop all services
	var errors []error
	for name := range c.services {
		if err := c.stopService(name); err != nil {
			errors = append(errors, fmt.Errorf("service %s: %w", name, err))
		}
	}

	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("Master controller shutdown complete")
	case <-ctx.Done():
		c.logger.Warn("Master controller shutdown timeout")
		return fmt.Errorf("shutdown timeout")
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}

	return nil
}

// Reload reloads the configuration and restarts affected services
func (c *Controller) Reload(newConfig *MasterConfig) error {
	c.logger.Info("Reloading master configuration")

	// Validate new configuration
	if err := newConfig.Validate(); err != nil {
		c.logger.Error("Invalid new configuration", zap.Error(err))
		return fmt.Errorf("invalid configuration: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Determine which services need to be started, stopped, or restarted
	oldServices := c.config.GetEnabledServices()
	newServices := newConfig.GetEnabledServices()

	// Stop services no longer enabled
	for name := range oldServices {
		if _, exists := newServices[name]; !exists {
			if err := c.stopService(name); err != nil {
				c.logger.Error("Failed to stop service during reload",
					zap.String("service", name),
					zap.Error(err))
			}
		}
	}

	// Start new services
	for name, svc := range newServices {
		if _, exists := oldServices[name]; !exists {
			if err := c.startService(name, svc); err != nil {
				c.logger.Error("Failed to start new service during reload",
					zap.String("service", name),
					zap.Error(err))
				return err
			}
		}
	}

	// Restart services with changed configuration
	for name, newSvc := range newServices {
		if oldSvc, exists := oldServices[name]; exists {
			if c.serviceConfigChanged(oldSvc, newSvc) {
				c.logger.Info("Restarting service due to config change",
					zap.String("service", name))

				if err := c.stopService(name); err != nil {
					c.logger.Error("Failed to stop service for restart",
						zap.String("service", name),
						zap.Error(err))
					continue
				}

				if err := c.startService(name, newSvc); err != nil {
					c.logger.Error("Failed to restart service",
						zap.String("service", name),
						zap.Error(err))
					return err
				}
			}
		}
	}

	// Update configuration
	c.config = newConfig

	c.updateStats(func(s *ControllerStats) {
		s.ConfigReloads++
		s.LastReload = time.Now()
		s.ServicesRunning = len(c.services)
	})

	c.logger.Info("Configuration reloaded successfully",
		zap.Int("services_running", len(c.services)))

	return nil
}

// serviceConfigChanged checks if service configuration has changed
func (c *Controller) serviceConfigChanged(old, new *Service) bool {
	// Check basic fields
	if old.Type != new.Type || old.Listen != new.Listen ||
		old.Workers != new.Workers || old.TLSMode != new.TLSMode {
		return true
	}

	// Check settings
	if old.Settings.RequireAuth != new.Settings.RequireAuth ||
		old.Settings.RequireTLS != new.Settings.RequireTLS ||
		old.Settings.AllowRelay != new.Settings.AllowRelay ||
		old.Settings.MaxMessageSize != new.Settings.MaxMessageSize {
		return true
	}

	// Check filters
	if len(old.Settings.Filters) != len(new.Settings.Filters) {
		return true
	}

	for i, f := range old.Settings.Filters {
		if new.Settings.Filters[i] != f {
			return true
		}
	}

	return false
}

// GetStats returns current controller statistics
func (c *Controller) GetStats() *ControllerStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()

	// Create a copy
	statsCopy := *c.stats
	return &statsCopy
}

// updateStats updates statistics safely
func (c *Controller) updateStats(fn func(*ControllerStats)) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	fn(c.stats)
}

// GetUptime returns the controller uptime
func (c *Controller) GetUptime() time.Duration {
	return time.Since(c.startTime)
}

// GetServiceStatus returns the status of all services
func (c *Controller) GetServiceStatus() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := make(map[string]string)
	for name := range c.services {
		status[name] = "running"
	}

	for name, svc := range c.config.Services {
		if !svc.Enabled {
			status[name] = "disabled"
		} else if _, running := c.services[name]; !running {
			status[name] = "stopped"
		}
	}

	return status
}
