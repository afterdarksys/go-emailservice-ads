package mailscript

import (
	"context"
	"fmt"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/routing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

var (
	mailscriptLatency = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "mailscript_evaluation_latency_seconds",
		Help:       "Latency of Starlark mailscript execution",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
)

// Engine runs MailScript policies.
type Engine struct {
	MaxSteps  uint64
	mapEngine *routing.MappingEngine
}

func NewEngine(mapEngine *routing.MappingEngine) *Engine {
	return &Engine{
		MaxSteps:  100000, // Defend against infinite loops
		mapEngine: mapEngine,
	}
}

// EvaluateScript runs a policy script against a specific AMP Message.
// Returns the disposition action ("accept", "reject", "discard", "fileinto", "vacation")
// and any associated data (like folder name or vacation body).
func (e *Engine) EvaluateScript(ctx context.Context, script string, msg *amp.AMPMessage) (Action, error) {
	start := time.Now()
	defer func() {
		mailscriptLatency.Observe(time.Since(start).Seconds())
	}()

	// Parse Script
	_, prog, err := starlark.SourceProgram("policy.star", script, func(name string) bool {
		return true // declare all unresolved globals as predefined
	})
	if err != nil {
		return Action{}, fmt.Errorf("compile error: %w", err)
	}

	// Prepare the execution state and builtins
	env := &ExecutionEnv{
		Message:   msg,
		MapEngine: e.mapEngine,
		Action: Action{
			Type: ActionAccept, // Default
		},
	}

	builtins := createBuiltins(env)

	thread := &starlark.Thread{
		Name: "mailscript",
	}
	thread.SetMaxExecutionSteps(e.MaxSteps)

	// Execute to register globals (including `evaluate` function)
	globals, err := prog.Init(thread, builtins)
	if err != nil {
		return Action{}, fmt.Errorf("script init failed: %w", err)
	}

	// Must contain `evaluate`
	evalFunc, ok := globals["evaluate"]
	if !ok {
		// Script didn't define evaluate(), just return default accept
		return env.Action, nil
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return Action{}, ctx.Err()
	default:
	}

	// Call `evaluate()`
	_, err = starlark.Call(thread, evalFunc, nil, nil)
	if err != nil {
		// If execution fails, fail-safe to accept or log
		return Action{}, fmt.Errorf("runtime error: %w", err)
	}

	return env.Action, nil
}

// Validate syntactically checks the script
func (e *Engine) Validate(script string) error {
	_, err := syntax.Parse("policy.star", script, 0)
	return err
}

type ActionType string

const (
	ActionAccept   ActionType = "accept"
	ActionReject   ActionType = "reject"
	ActionDiscard  ActionType = "discard"
	ActionFileinto ActionType = "fileinto"
	ActionRedirect ActionType = "redirect"
	ActionVacation ActionType = "vacation"
)

type Action struct {
	Type     ActionType
	Arg      string // e.g. "Junk" for fileinto, destination for redirect, error for reject
	Body     string // used for vacation
	Days     int    // used for vacation
	AddFlags []string
	RemFlags []string
}

// ExecutionEnv holds the mutable state during a single script execution
type ExecutionEnv struct {
	Message   *amp.AMPMessage
	MapEngine *routing.MappingEngine
	Action    Action
}
