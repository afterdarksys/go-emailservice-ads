package mailscript

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
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
		"http_get":     starlark.NewBuiltin("http_get", builtinHTTPGet(logger)),
		"regex_match":  starlark.NewBuiltin("regex_match", builtinRegexMatch(logger)),
		"dlp_scan":     starlark.NewBuiltin("dlp_scan", builtinDLPScan(logger)),
	}

	return &Engine{
		logger:  logger,
		globals: globals,
	}
}

// MailAction represents actions a script can take on an email
type MailAction struct {
	Reject     bool
	Quarantine bool
	Drop       bool
	Deliver    bool
	RedirectTo string
	Reason     string
	// Modifications
	AddHeaders    map[string]string
	RemoveHeaders []string
	ModifyBody    string // Only text/plain for now
}

// ExecutePolicy runs a Starlark script against the given context map
// Returns actions the engine should take based on the script
func (e *Engine) ExecutePolicy(scriptName, scriptContent string, mailCtx map[string]interface{}) (*MailAction, error) {
	thread := &starlark.Thread{Name: "mailscript_thread"}
	action := &MailAction{
		AddHeaders: make(map[string]string),
	}

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

	// Mail object for mutation
	mailObj := &starlarkMailObj{action: action}
	env["mail"] = mailObj

	e.logger.Debug("Executing MailScript Policy", zap.String("script", scriptName))

	_, err := starlark.ExecFile(thread, scriptName, scriptContent, env)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			e.logger.Error("MailScript execution error", zap.String("msg", evalErr.Backtrace()))
		}

		// check if the script explicitly aborted/rejected
		if strings.Contains(err.Error(), "REJECTED_BY_POLICY") {
			parts := strings.Split(err.Error(), "REJECTED_BY_POLICY: ")
			reason := "Rejected by policy"
			if len(parts) > 1 {
				reason = parts[1]
			}
			action.Reject = true
			action.Reason = reason
			return action, nil
		}

		return action, fmt.Errorf("script error: %w", err)
	}

	return action, nil
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

func builtinHTTPGet(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var url string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &url); err != nil {
			return starlark.None, err
		}
		
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			logger.Warn("Mailscript HTTP GET failed", zap.Error(err))
			return starlark.String(""), nil
		}
		defer resp.Body.Close()
		
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return starlark.String(""), nil
		}
		
		return starlark.String(string(bodyBytes)), nil
	}
}

func builtinRegexMatch(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var pattern string
		var text string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "text", &text); err != nil {
			return starlark.None, err
		}
		
		matched, err := regexp.MatchString(pattern, text)
		if err != nil {
			return starlark.Bool(false), nil // Ignore malformed regex
		}
		return starlark.Bool(matched), nil
	}
}

func builtinDLPScan(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var text string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "text", &text); err != nil {
			return starlark.None, err
		}
		
		// Basic DLP matching, looking for primitives (SSN, CC numbers, etc)
		// This should be integrated via gRPC to the main `opendlp` cluster
		// For the Mailscript prototype, use simplified regex patterns for PII.
		hasCC, _ := regexp.MatchString(`\b(?:\d[ -]*?){13,16}\b`, text) // Basic CC
		hasSSN, _ := regexp.MatchString(`\b\d{3}-\d{2}-\d{4}\b`, text) // Basic SSN
		
		res := starlark.NewDict(5)
		res.SetKey(starlark.String("has_pii"), starlark.Bool(hasCC || hasSSN))
		res.SetKey(starlark.String("has_credit_card"), starlark.Bool(hasCC))
		res.SetKey(starlark.String("has_ssn"), starlark.Bool(hasSSN))
		return res, nil
	}
}

func builtinMimeExtract(logger *zap.Logger) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var rawEmail string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "raw_email", &rawEmail); err != nil {
			return starlark.None, err
		}
		logger.Debug("MailScript requesting MIME extraction")

		reader := strings.NewReader(rawEmail)
		mr, err := mail.CreateReader(reader)
		if err != nil {
			logger.Warn("Mailscript Failed to parse raw email", zap.Error(err))
			return starlark.NewList(nil), nil
		}
		
		parts := starlark.NewList(nil)
		
		// Process each message's part
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				continue
			}

			b, _ := io.ReadAll(p.Body)
			
			partDict := starlark.NewDict(5)
			ctype, _, _ := p.Header.ContentType()
			partDict.SetKey(starlark.String("content_type"), starlark.String(ctype))
			partDict.SetKey(starlark.String("body"), starlark.String(string(b)))
			
			parts.Append(partDict)
		}
		
		return parts, nil
	}
}

// --- Starlark Mail Object (Methods for mutating the email object) ---

type starlarkMailObj struct {
	action *MailAction
}

func (m *starlarkMailObj) String() string        { return "<mail>" }
func (m *starlarkMailObj) Type() string          { return "mail" }
func (m *starlarkMailObj) Freeze()               {}
func (m *starlarkMailObj) Truth() starlark.Bool  { return starlark.True }
func (m *starlarkMailObj) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: mail") }

// Attr implements starlark.HasAttrs interface, enabling `mail.add_header(...)`
func (m *starlarkMailObj) AttrNames() []string {
	return []string{
		"add_header", "remove_header", "replace_body",
		"quarantine", "drop", "deliver", "redirect",
	}
}

func (m *starlarkMailObj) Attr(name string) (starlark.Value, error) {
	switch name {
	case "add_header":
		return starlark.NewBuiltin("add_header", m.addHeader), nil
	case "remove_header":
		return starlark.NewBuiltin("remove_header", m.removeHeader), nil
	case "replace_body":
		return starlark.NewBuiltin("replace_body", m.replaceBody), nil
	case "quarantine":
		return starlark.NewBuiltin("quarantine", m.quarantine), nil
	case "drop":
		return starlark.NewBuiltin("drop", m.drop), nil
	case "deliver":
		return starlark.NewBuiltin("deliver", m.deliver), nil
	case "redirect":
		return starlark.NewBuiltin("redirect", m.redirect), nil
	default:
		return nil, nil // attribute not found
	}
}

// Mail Object Builtins
func (m *starlarkMailObj) addHeader(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key, val string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key, "value", &val); err != nil {
		return starlark.None, err
	}
	m.action.AddHeaders[key] = val
	return starlark.None, nil
}

func (m *starlarkMailObj) removeHeader(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return starlark.None, err
	}
	m.action.RemoveHeaders = append(m.action.RemoveHeaders, key)
	return starlark.None, nil
}

func (m *starlarkMailObj) replaceBody(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var body string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "body", &body); err != nil {
		return starlark.None, err
	}
	m.action.ModifyBody = body
	return starlark.None, nil
}

func (m *starlarkMailObj) quarantine(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reason string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "reason", &reason); err != nil {
		return starlark.None, err
	}
	m.action.Quarantine = true
	m.action.Reason = reason
	return starlark.None, nil
}

func (m *starlarkMailObj) drop(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	m.action.Drop = true
	return starlark.None, nil
}

func (m *starlarkMailObj) deliver(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	m.action.Deliver = true
	return starlark.None, nil
}

func (m *starlarkMailObj) redirect(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var to string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "to", &to); err != nil {
		return starlark.None, err
	}
	m.action.RedirectTo = to
	return starlark.None, nil
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
