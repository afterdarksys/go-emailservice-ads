package policy

import (
	"context"
)

// sieveEngine implements the Sieve mail filtering language (RFC 5228)
type sieveEngine struct {
	capabilities []string
}

// newSieveEngine creates a new Sieve engine
func newSieveEngine() (*sieveEngine, error) {
	return &sieveEngine{
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
	ast, ok := compiled.(*svScript)
	if !ok {
		return &Action{Type: ActionKeep}, nil
	}
	return svExec(emailCtx, ast)
}

func (e *sieveEngine) Validate(script string) error {
	_, err := svParse(script)
	return err
}

func (e *sieveEngine) GetCapabilities() []string {
	return e.capabilities
}
