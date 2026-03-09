package starlark

import (
	"context"
	"fmt"

	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Engine implements the Starlark scripting engine for email policies
type Engine struct {
	// Execution options
	maxSteps int64 // Maximum execution steps (0 = unlimited)
}

// NewEngine creates a new Starlark engine
func NewEngine() (*Engine, error) {
	return &Engine{
		maxSteps: 100000, // Reasonable limit to prevent infinite loops
	}, nil
}

// GetType returns the engine type
func (e *Engine) GetType() policy.PolicyType {
	return policy.PolicyTypeStarlark
}

// Evaluate executes a Starlark script against an email context
func (e *Engine) Evaluate(ctx context.Context, emailCtx *policy.EmailContext, script string) (*policy.Action, error) {
	// Compile and execute
	compiled, err := e.Compile(script)
	if err != nil {
		return nil, err
	}
	return e.ExecuteCompiled(ctx, emailCtx, compiled)
}

// compiledScript holds a pre-compiled Starlark program
type compiledScript struct {
	prog *starlark.Program
}

// Compile pre-compiles a Starlark script
func (e *Engine) Compile(script string) (interface{}, error) {
	// Parse the script
	_, prog, err := starlark.SourceProgram("policy.star", script, func(name string) bool {
		// Predeclared names (built-ins) - will be provided at runtime
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compile script: %w", err)
	}

	return &compiledScript{prog: prog}, nil
}

// ExecuteCompiled executes a pre-compiled Starlark script
func (e *Engine) ExecuteCompiled(ctx context.Context, emailCtx *policy.EmailContext, compiled interface{}) (*policy.Action, error) {
	cs, ok := compiled.(*compiledScript)
	if !ok {
		return nil, fmt.Errorf("invalid compiled script type")
	}

	// Reset global action state
	globalAction = nil
	globalHeaders = nil

	// Create built-in functions specific to this email context
	builtins := createBuiltins(emailCtx)

	// Create thread with execution limits
	thread := &starlark.Thread{
		Name: "policy",
	}

	// Set step limit to prevent infinite loops
	if e.maxSteps > 0 {
		thread.SetMaxExecutionSteps(e.maxSteps)
	}

	// Execute the program
	globals, err := cs.prog.Init(thread, builtins)
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w", err)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	_ = globals // Script may not define any globals, that's ok

	// Return the action set by the script
	if globalAction == nil {
		// No explicit action - default to keep
		return &policy.Action{
			Type:    policy.ActionKeep,
			Headers: globalHeaders,
		}, nil
	}

	return globalAction, nil
}

// Validate checks if a Starlark script is syntactically correct
func (e *Engine) Validate(script string) error {
	// Parse the script to check syntax
	_, err := syntax.Parse("policy.star", script, 0)
	if err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}

	// Try to compile
	_, err = e.Compile(script)
	if err != nil {
		return err
	}

	return nil
}

// GetCapabilities returns the capabilities supported by this engine
func (e *Engine) GetCapabilities() []string {
	return []string{
		"email_inspection",
		"security_checks",
		"reputation_lookups",
		"dns_queries",
		"group_membership",
		"header_manipulation",
		"content_filtering",
		"regex_matching",
		"notifications",
	}
}
