package routing

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/routing/divert"
	"github.com/afterdarksys/go-emailservice-ads/internal/routing/groups"
	"github.com/afterdarksys/go-emailservice-ads/internal/routing/screen"
)

// Pipeline integrates divert and screen systems into mail flow
type Pipeline struct {
	logger        *zap.Logger
	groupManager  *groups.Manager
	divertEngine  *divert.Engine
	screenEngine  *screen.Engine
	enabled       bool
}

// PipelineConfig configures the routing pipeline
type PipelineConfig struct {
	GroupsConfigPath  string
	DivertConfigPath  string
	ScreenConfigPath  string
	EnableDivert      bool
	EnableScreen      bool
}

// NewPipeline creates a new routing pipeline
func NewPipeline(config *PipelineConfig, logger *zap.Logger) (*Pipeline, error) {
	var groupManager *groups.Manager
	var divertEngine *divert.Engine
	var screenEngine *screen.Engine
	var err error

	// Initialize groups manager (required for both divert and screen)
	if config.EnableDivert || config.EnableScreen {
		groupManager, err = groups.NewManager(config.GroupsConfigPath, logger)
		if err != nil {
			logger.Warn("Failed to initialize groups manager, continuing without groups",
				zap.Error(err))
			// Continue without groups - rules won't work but system still functions
		}
	}

	// Initialize divert engine
	if config.EnableDivert {
		divertEngine, err = divert.NewEngine(config.DivertConfigPath, logger, groupManager)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize divert engine: %w", err)
		}
		logger.Info("Divert engine initialized and enabled")
	}

	// Initialize screen engine
	if config.EnableScreen {
		screenEngine, err = screen.NewEngine(config.ScreenConfigPath, logger, groupManager)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize screen engine: %w", err)
		}
		logger.Info("Screen engine initialized and enabled")
	}

	p := &Pipeline{
		logger:       logger,
		groupManager: groupManager,
		divertEngine: divertEngine,
		screenEngine: screenEngine,
		enabled:      config.EnableDivert || config.EnableScreen,
	}

	logger.Info("Routing pipeline initialized",
		zap.Bool("divert_enabled", config.EnableDivert),
		zap.Bool("screen_enabled", config.EnableScreen))

	return p, nil
}

// RoutingDecision represents the result of routing checks
type RoutingDecision struct {
	// Divert decision
	ShouldDivert   bool
	DivertTo       string
	DivertReason   string
	DivertedData   []byte

	// Screen decision
	ShouldScreen   bool
	Watchers       []string

	// Error
	Error error
}

// Process checks a message through the routing pipeline
// Returns a RoutingDecision indicating what should happen
func (p *Pipeline) Process(ctx context.Context, from, to string, data []byte) *RoutingDecision {
	decision := &RoutingDecision{}

	if !p.enabled {
		return decision // No routing configured
	}

	// Step 1: Check screening first (it's transparent, so check it regardless)
	if p.screenEngine != nil {
		shouldScreen, watchers, err := p.screenEngine.CheckScreen(ctx, from, to, data)
		if err != nil {
			p.logger.Error("Screen check failed",
				zap.String("from", from),
				zap.String("to", to),
				zap.Error(err))
			decision.Error = fmt.Errorf("screen check failed: %w", err)
			return decision
		}

		if shouldScreen {
			decision.ShouldScreen = true
			decision.Watchers = watchers

			p.logger.Info("Message will be screened",
				zap.String("from", from),
				zap.String("to", to),
				zap.Int("watchers", len(watchers)))
		}
	}

	// Step 2: Check divert (this overrides normal delivery)
	if p.divertEngine != nil {
		shouldDivert, divertTo, reason, err := p.divertEngine.CheckDivert(ctx, from, to, data)
		if err != nil {
			p.logger.Error("Divert check failed",
				zap.String("from", from),
				zap.String("to", to),
				zap.Error(err))
			decision.Error = fmt.Errorf("divert check failed: %w", err)
			return decision
		}

		if shouldDivert {
			// Create diverted message
			divertedData, err := p.divertEngine.ProcessDivert(ctx, from, to, divertTo, reason, data)
			if err != nil {
				p.logger.Error("Divert processing failed",
					zap.String("from", from),
					zap.String("to", to),
					zap.String("divert_to", divertTo),
					zap.Error(err))
				decision.Error = fmt.Errorf("divert processing failed: %w", err)
				return decision
			}

			decision.ShouldDivert = true
			decision.DivertTo = divertTo
			decision.DivertReason = reason
			decision.DivertedData = divertedData

			p.logger.Info("Message will be diverted",
				zap.String("from", from),
				zap.String("original_to", to),
				zap.String("diverted_to", divertTo),
				zap.String("reason", reason))
		}
	}

	return decision
}

// ProcessScreen sends screened copies to watchers
func (p *Pipeline) ProcessScreen(ctx context.Context, from, to string, watchers []string, data []byte) error {
	if p.screenEngine == nil {
		return fmt.Errorf("screen engine not initialized")
	}

	return p.screenEngine.ProcessScreen(ctx, from, to, watchers, data)
}

// ExpandRecipients expands group references in recipient list
func (p *Pipeline) ExpandRecipients(ctx context.Context, recipients []string) ([]string, error) {
	if p.groupManager == nil {
		return recipients, nil // No expansion without groups
	}

	return p.groupManager.ExpandRecipients(ctx, recipients)
}

// Reload reloads all routing configurations
func (p *Pipeline) Reload() error {
	var errors []error

	if p.groupManager != nil {
		if err := p.groupManager.Reload(); err != nil {
			p.logger.Error("Failed to reload groups", zap.Error(err))
			errors = append(errors, fmt.Errorf("groups reload: %w", err))
		}
	}

	if p.divertEngine != nil {
		if err := p.divertEngine.Reload(); err != nil {
			p.logger.Error("Failed to reload divert config", zap.Error(err))
			errors = append(errors, fmt.Errorf("divert reload: %w", err))
		}
	}

	if p.screenEngine != nil {
		if err := p.screenEngine.Reload(); err != nil {
			p.logger.Error("Failed to reload screen config", zap.Error(err))
			errors = append(errors, fmt.Errorf("screen reload: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("routing reload errors: %v", errors)
	}

	p.logger.Info("Routing pipeline reloaded successfully")
	return nil
}

// IsEnabled returns whether routing is enabled
func (p *Pipeline) IsEnabled() bool {
	return p.enabled
}

// GetStats returns routing pipeline statistics
func (p *Pipeline) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled": p.enabled,
		"divert_enabled": p.divertEngine != nil,
		"screen_enabled": p.screenEngine != nil,
		"groups_enabled": p.groupManager != nil,
	}

	if p.groupManager != nil {
		stats["groups_count"] = len(p.groupManager.ListGroups())
	}

	return stats
}
