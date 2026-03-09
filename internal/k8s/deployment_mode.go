package k8s

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// DeploymentMode represents the mode the SMTP server is running in
type DeploymentMode string

const (
	// ModePerimeter is an internet-facing MTA with heavy security
	ModePerimeter DeploymentMode = "perimeter"

	// ModeInternal is an internal mail hub for trusted traffic
	ModeInternal DeploymentMode = "internal"

	// ModeHybrid supports both perimeter and internal roles
	ModeHybrid DeploymentMode = "hybrid"

	// ModeStandalone is a standalone server (non-Kubernetes)
	ModeStandalone DeploymentMode = "standalone"
)

// DeploymentConfig holds configuration for the current deployment mode
type DeploymentConfig struct {
	Mode   DeploymentMode
	Region string
	Zone   string

	// Perimeter settings
	EnableGreylisting    bool
	RequireTLS           bool
	RateLimitPerIP       int
	MaxConnectionsPerIP  int
	EnableRBL            bool
	EnableReputation     bool

	// Internal settings
	RequireAuth       bool
	TrustedNetworks   []string
	EnableLDAP        bool
	SkipSPFDKIM       bool

	// Kubernetes integration
	KubernetesEnabled bool
	Namespace         string
	ServiceDiscovery  bool
	EndpointWatching  bool

	// Global routing
	EnableGlobalRouting bool
	StateStoreType      string
	StateStoreEndpoints []string
}

// DetectDeploymentMode determines the deployment mode from environment variables
func DetectDeploymentMode(logger *zap.Logger) (*DeploymentConfig, error) {
	config := &DeploymentConfig{
		Mode:   ModeStandalone,
		Region: os.Getenv("REGION"),
		Zone:   os.Getenv("ZONE"),
	}

	// Check for Kubernetes environment
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		config.KubernetesEnabled = true
		config.Namespace = os.Getenv("POD_NAMESPACE")
		if config.Namespace == "" {
			config.Namespace = "default"
		}

		// Get pod labels for region/zone if not set by env vars
		if config.Region == "" {
			config.Region = os.Getenv("POD_REGION")
		}
		if config.Zone == "" {
			config.Zone = os.Getenv("POD_ZONE")
		}

		logger.Info("Detected Kubernetes environment",
			zap.String("namespace", config.Namespace),
			zap.String("region", config.Region),
			zap.String("zone", config.Zone))
	}

	// Determine deployment mode
	modeEnv := strings.ToLower(os.Getenv("DEPLOYMENT_MODE"))
	switch modeEnv {
	case "perimeter":
		config.Mode = ModePerimeter
		config.configurePerimeter()
		logger.Info("Running in PERIMETER mode (internet-facing)")

	case "internal":
		config.Mode = ModeInternal
		config.configureInternal()
		logger.Info("Running in INTERNAL mode (internal hub)")

	case "hybrid":
		config.Mode = ModeHybrid
		config.configurePerimeter()
		config.configureInternal()
		logger.Info("Running in HYBRID mode (perimeter + internal)")

	case "", "standalone":
		config.Mode = ModeStandalone
		config.configureStandalone()
		logger.Info("Running in STANDALONE mode")

	default:
		return nil, fmt.Errorf("invalid DEPLOYMENT_MODE: %s (must be perimeter, internal, hybrid, or standalone)", modeEnv)
	}

	// Service discovery settings
	if envBool("ENABLE_SERVICE_DISCOVERY", true) {
		config.ServiceDiscovery = true
	}
	if envBool("ENABLE_ENDPOINT_WATCHING", true) {
		config.EndpointWatching = true
	}

	// Global routing settings
	if envBool("ENABLE_GLOBAL_ROUTING", false) {
		config.EnableGlobalRouting = true
		config.StateStoreType = envString("STATE_STORE_TYPE", "etcd")
		config.StateStoreEndpoints = envStringSlice("STATE_STORE_ENDPOINTS", []string{"etcd:2379"})
		logger.Info("Global routing enabled",
			zap.String("state_store", config.StateStoreType),
			zap.Strings("endpoints", config.StateStoreEndpoints))
	}

	return config, nil
}

// configurePerimeter sets perimeter-specific configuration
func (c *DeploymentConfig) configurePerimeter() {
	c.EnableGreylisting = envBool("ENABLE_GREYLISTING", true)
	c.RequireTLS = envBool("REQUIRE_TLS", true)
	c.RateLimitPerIP = envInt("RATE_LIMIT_PER_IP", 100)
	c.MaxConnectionsPerIP = envInt("MAX_CONNECTIONS_PER_IP", 10)
	c.EnableRBL = envBool("ENABLE_RBL", true)
	c.EnableReputation = envBool("ENABLE_REPUTATION", true)
}

// configureInternal sets internal hub-specific configuration
func (c *DeploymentConfig) configureInternal() {
	c.RequireAuth = envBool("REQUIRE_AUTH", true)
	c.TrustedNetworks = envStringSlice("TRUSTED_NETWORKS", []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	c.EnableLDAP = envBool("ENABLE_LDAP", false)
	c.SkipSPFDKIM = envBool("SKIP_SPF_DKIM", true)
}

// configureStandalone sets standalone-specific configuration
func (c *DeploymentConfig) configureStandalone() {
	// Standalone uses config file for all settings
	c.KubernetesEnabled = false
	c.ServiceDiscovery = false
	c.EndpointWatching = false
	c.EnableGlobalRouting = false
}

// IsPerimeter returns true if running in perimeter or hybrid mode
func (c *DeploymentConfig) IsPerimeter() bool {
	return c.Mode == ModePerimeter || c.Mode == ModeHybrid
}

// IsInternal returns true if running in internal or hybrid mode
func (c *DeploymentConfig) IsInternal() bool {
	return c.Mode == ModeInternal || c.Mode == ModeHybrid
}

// IsKubernetes returns true if running in Kubernetes
func (c *DeploymentConfig) IsKubernetes() bool {
	return c.KubernetesEnabled
}

// Helper functions to read environment variables

func envBool(key string, defaultValue bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val == "true" || val == "1" || val == "yes"
}

func envInt(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	var result int
	fmt.Sscanf(val, "%d", &result)
	if result == 0 {
		return defaultValue
	}
	return result
}

func envString(key string, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

func envStringSlice(key string, defaultValue []string) []string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return strings.Split(val, ",")
}
