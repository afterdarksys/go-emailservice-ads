package mailscript

import (
	"encoding/base64"
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.uber.org/zap"
)

// Engine defines the execution environment for MailScript (Starlark)
type Engine struct {
	logger  *zap.Logger
	globals starlark.StringDict
}

func NewEngine(logger *zap.Logger) *Engine {
	// Expose platform features, DLP hooks, etc into globals
	globals := starlark.StringDict{
		"reject":       starlark.NewBuiltin("reject", builtinReject),
		"log":          starlark.NewBuiltin("log", builtinLog(logger)),
		"b64encode":    starlark.NewBuiltin("b64encode", builtinB64Encode),
		"b64decode":    starlark.NewBuiltin("b64decode", builtinB64Decode),
		"mime_extract": starlark.NewBuiltin("mime_extract", builtinMimeExtract(logger)),
	}

	return &Engine{
		logger:  logger,
		globals: globals,
	}
}

// ExecutePolicy runs a Starlark script against the given context map
func (e *Engine) ExecutePolicy(scriptName, scriptContent string, mailCtx map[string]interface{}) (bool, error) {
	thread := &starlark.Thread{Name: "mailscript_thread"}

	// Set contextual variables for this run
	env := make(starlark.StringDict)
	for k, v := range e.globals {
		env[k] = v
	}

	ctxDict := starlark.NewDict(10)
	for k, v := range mailCtx {
		val, err := toStarlarkValue(v)
		if err == nil {
			ctxDict.SetKey(starlark.String(k), val)
		}
	}
	env["ctx"] = ctxDict

	e.logger.Debug("Executing MailScript Policy", zap.String("script", scriptName))

	_, err := starlark.ExecFile(thread, scriptName, scriptContent, env)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			e.logger.Error("MailScript execution error", zap.String("msg", evalErr.Backtrace()))
		}

		// check if the script explicitly aborted/rejected
		if strings.Contains(err.Error(), "REJECTED_BY_POLICY") {
			return false, nil // false = don't proceed with email
		}

		return false, fmt.Errorf("script error: %w", err)
	}

	return true, nil // allowed
}

// Helpers for Starlark
func builtinReject(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reason string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "reason", &reason); err != nil {
		return starlark.None, err
	}
	return starlark.None, fmt.Errorf("REJECTED_BY_POLICY: %s", reason)
}

func builtinLog(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var msg string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "msg", &msg); err != nil {
			return starlark.None, err
		}
		logger.Info("MailScript Log", zap.String("msg", msg))
		return starlark.None, nil
	}
}

func builtinB64Encode(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return starlark.None, err
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	return starlark.String(encoded), nil
}

func builtinB64Decode(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "data", &data); err != nil {
		return starlark.None, err
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return starlark.None, err
	}
	return starlark.String(decoded), nil
}

// builtinMimeExtract represents a stub/binding for `go-message` MIME extraction logic.
func builtinMimeExtract(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var rawEmail string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "raw_email", &rawEmail); err != nil {
			return starlark.None, err
		}
		logger.Debug("MailScript requesting MIME extraction")
		// TODO: Implement `github.com/emersion/go-message/mail` reading and attachment extraction.
		// Returns an array/dict of attachments.

		ret := starlark.NewList(nil)
		return ret, nil
	}
}

// convert Go values to Starlark values
func toStarlarkValue(val interface{}) (starlark.Value, error) {
	switch v := val.(type) {
	case string:
		return starlark.String(v), nil
	case int:
		return starlark.MakeInt(v), nil
	case bool:
		return starlark.Bool(v), nil
	// Add list/dict as needed
	default:
		return starlark.String(fmt.Sprintf("%v", v)), nil
	}
}
