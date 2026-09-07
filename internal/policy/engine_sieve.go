package policy

import (
	"context"
	"fmt"
)

// sieveEngine implements the Sieve mail filtering language (RFC 5228)
type sieveEngine struct {
	capabilities []string
}

// newSieveEngine creates a new Sieve engine
func newSieveEngine() (*sieveEngine, error) {
	return &sieveEngine{
		capabilities: []string{"fileinto", "reject", "envelope", "body", "variables", "imap4flags", "copy", "vacation"},
	}, nil
}

func (e *sieveEngine) GetType() PolicyType {
	return PolicyTypeSieve
}

func (e *sieveEngine) Evaluate(ctx context.Context, emailCtx *EmailContext, script string) (*Action, error) {
	compiled, err := e.Compile(script)
	if err != nil {
		return nil, err
	}
	return e.ExecuteCompiled(ctx, emailCtx, compiled)
}

func (e *sieveEngine) Compile(script string) (interface{}, error) {
	return svParse(script)
}

func (e *sieveEngine) ExecuteCompiled(ctx context.Context, emailCtx *EmailContext, compiled interface{}) (*Action, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ast, ok := compiled.(*svScript)
	if !ok {
		return nil, fmt.Errorf("invalid compiled Sieve script")
	}
	action, err := svExec(ctx, emailCtx, ast)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return action, err
}

func (e *sieveEngine) Validate(script string) error {
	_, err := svParse(script)
	return err
}

func (e *sieveEngine) GetCapabilities() []string {
	return e.capabilities
}

// ValidateSieve checks a script using the delivery engine's supported grammar.
func ValidateSieve(script string) error { _, err := svParse(script); return err }
