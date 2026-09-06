package policy

import (
	"context"
	"fmt"

	"go.starlark.net/starlark"
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
	if len(script) > 1<<20 {
		return nil, fmt.Errorf("Starlark script exceeds 1 MiB")
	}
	names := createStarlarkBuiltins(&EmailContext{})
	_, prog, err := starlark.SourceProgram("policy.star", script, func(name string) bool {
		_, exists := names[name]
		return exists
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
	if emailCtx == nil {
		return nil, fmt.Errorf("email context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create built-ins
	builtins := createStarlarkBuiltins(emailCtx)

	thread := &starlark.Thread{
		Name:  "policy",
		Print: appendTrace,
	}

	thread.SetLocal("context", ctx)
	if e.maxSteps > 0 {
		thread.SetMaxExecutionSteps(uint64(e.maxSteps))
	}

	stop := context.AfterFunc(ctx, func() { thread.Cancel(ctx.Err().Error()) })
	defer stop()
	globals, err := cs.prog.Init(thread, builtins)
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w", err)
	}
	if filter, exists := globals["filter"]; exists {
		callable, ok := filter.(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("filter must be a function")
		}
		value, err := starlark.Call(thread, callable, starlark.Tuple{builtins["message"]}, nil)
		if err != nil {
			return nil, fmt.Errorf("filter execution failed: %w", err)
		}
		if value != starlark.None {
			return nil, fmt.Errorf("filter must return None; use action builtins")
		}
	}
	// DNS helpers preserve legacy empty-result semantics for DNS failures, but
	// exhausting the policy's budget must fail evaluation rather than accept mail.
	if state(thread).DNSLookups > maxPolicyDNSLookups {
		return nil, fmt.Errorf("policy DNS lookup budget exceeded")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return ThreadAction(thread), nil
}

func (e *starlarkEngine) Validate(script string) error {
	_, err := e.Compile(script)
	return err
}

func (e *starlarkEngine) GetCapabilities() []string {
	return []string{
		"immutable_message", "filter_entrypoint", "cidr_matching", "bounded_trace",
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
