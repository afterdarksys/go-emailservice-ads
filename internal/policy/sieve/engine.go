package sieve

import (
	"context"
	"fmt"
)

// PolicyType is duplicated here to avoid import cycle
type PolicyType string

const PolicyTypeSieve PolicyType = "sieve"

// Engine implements the Sieve mail filtering language (RFC 5228)
type Engine struct {
	capabilities []string
}

// NewEngine creates a new Sieve engine
func NewEngine() (*Engine, error) {
	return &Engine{
		capabilities: []string{
			"fileinto",       // RFC 5228
			"reject",         // RFC 5429
			"envelope",       // RFC 5228
			"body",           // RFC 5173
			"variables",      // RFC 5229
			"vacation",       // RFC 5230
			"relational",     // RFC 5231
			"comparator-i;ascii-numeric", // RFC 5231
			"imap4flags",     // RFC 5232
			"subaddress",     // RFC 5233
			"copy",           // RFC 3894
			"editheader",     // RFC 5293
		},
	}, nil
}

// GetType returns the engine type
func (e *Engine) GetType() PolicyType {
	return PolicyTypeSieve
}

// EmailContext is forward-declared to avoid import cycle
type EmailContext interface{}
type Action interface{}

// Evaluate executes a Sieve script against an email context
func (e *Engine) Evaluate(ctx context.Context, emailCtx EmailContext, script string) (Action, error) {
	// Compile and execute
	compiled, err := e.Compile(script)
	if err != nil {
		return nil, err
	}
	return e.ExecuteCompiled(ctx, emailCtx, compiled)
}

// Compile pre-compiles a Sieve script
func (e *Engine) Compile(script string) (interface{}, error) {
	// TODO: Implement Sieve script parsing and compilation
	// For now, just store the script
	return script, nil
}

// ExecuteCompiled executes a pre-compiled Sieve script
func (e *Engine) ExecuteCompiled(ctx context.Context, emailCtx EmailContext, compiled interface{}) (Action, error) {
	script, ok := compiled.(string)
	if !ok {
		return nil, fmt.Errorf("invalid compiled script type")
	}

	// TODO: Implement Sieve script execution
	// This is a placeholder implementation

	// For now, return a default keep action
	_ = script // Use the script variable
	_ = emailCtx // Use the context

	// Return nil for now - will be properly implemented later
	return nil, nil
}

// Validate checks if a Sieve script is syntactically correct
func (e *Engine) Validate(script string) error {
	// TODO: Implement Sieve script validation
	// For now, accept all scripts
	_ = script
	return nil
}

// GetCapabilities returns the Sieve capabilities supported
func (e *Engine) GetCapabilities() []string {
	return e.capabilities
}
