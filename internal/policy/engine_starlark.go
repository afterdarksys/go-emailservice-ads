package policy

import (
	"context"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// starlarkEngine implements the Starlark scripting engine for email policies
type starlarkEngine struct {
	maxSteps int64
}

// newStarlarkEngine creates a new Starlark engine
func newStarlarkEngine() (*starlarkEngine, error) {
	return &starlarkEngine{
		maxSteps: 100000,
	}, nil
}

func (e *starlarkEngine) GetType() PolicyType {
	return PolicyTypeStarlark
}

type compiledStarlarkScript struct {
	prog *starlark.Program
}

func (e *starlarkEngine) Compile(script string) (interface{}, error) {
	_, prog, err := starlark.SourceProgram("policy.star", script, func(name string) bool {
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compile script: %w", err)
	}
	return &compiledStarlarkScript{prog: prog}, nil
}

func (e *starlarkEngine) Evaluate(ctx context.Context, emailCtx *EmailContext, script string) (*Action, error) {
	compiled, err := e.Compile(script)
	if err != nil {
		return nil, err
	}
	return e.ExecuteCompiled(ctx, emailCtx, compiled)
}

func (e *starlarkEngine) ExecuteCompiled(ctx context.Context, emailCtx *EmailContext, compiled interface{}) (*Action, error) {
	cs, ok := compiled.(*compiledStarlarkScript)
	if !ok {
		return nil, fmt.Errorf("invalid compiled script type")
	}

	// Reset global state
	globalAction = nil
	globalHeaders = nil

	// Create built-ins
	builtins := createStarlarkBuiltins(emailCtx)

	thread := &starlark.Thread{
		Name: "policy",
	}

	if e.maxSteps > 0 {
		thread.SetMaxExecutionSteps(uint64(e.maxSteps))
	}

	_, err := cs.prog.Init(thread, builtins)
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if globalAction == nil {
		return &Action{
			Type:    ActionKeep,
			Headers: globalHeaders,
		}, nil
	}

	return globalAction, nil
}

func (e *starlarkEngine) Validate(script string) error {
	_, err := syntax.Parse("policy.star", script, 0)
	if err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}

	_, err = e.Compile(script)
	return err
}

func (e *starlarkEngine) GetCapabilities() []string {
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
