package policy

import (
	"context"
	"fmt"
)

// Engine defines the interface for policy execution engines (Sieve, Starlark)
type Engine interface {
	// GetType returns the engine type ("sieve" or "starlark")
	GetType() PolicyType

	// Evaluate executes a policy script against an email context
	Evaluate(ctx context.Context, emailCtx *EmailContext, script string) (*Action, error)

	// Compile pre-compiles a script for faster execution (optional optimization)
	Compile(script string) (interface{}, error)

	// ExecuteCompiled executes a pre-compiled script
	ExecuteCompiled(ctx context.Context, emailCtx *EmailContext, compiled interface{}) (*Action, error)

	// Validate checks if a script is syntactically correct
	Validate(script string) error

	// GetCapabilities returns the capabilities/extensions supported by this engine
	GetCapabilities() []string
}

// NewEngine creates a policy engine of the specified type
func NewEngine(engineType PolicyType) (Engine, error) {
	switch engineType {
	case PolicyTypeSieve:
		return newSieveEngine()
	case PolicyTypeStarlark:
		return newStarlarkEngine()
	default:
		return nil, fmt.Errorf("unknown policy engine type: %s", engineType)
	}
}
