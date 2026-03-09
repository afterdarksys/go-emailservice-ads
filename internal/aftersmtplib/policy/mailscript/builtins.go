package mailscript

import (
	"regexp"
	"strings"

	"go.starlark.net/starlark"
)

// createBuiltins returns the dictionary of predefined functions injected into the script.
func createBuiltins(env *ExecutionEnv) starlark.StringDict {
	return starlark.StringDict{
		// Context retrieval
		"get_header":              starlark.NewBuiltin("get_header", env.builtinGetHeader),
		"get_sender_did":          starlark.NewBuiltin("get_sender_did", env.builtinGetSenderDid),
		"get_recipient_did":       starlark.NewBuiltin("get_recipient_did", env.builtinGetRecipientDid),
		"verify_blockchain_proof": starlark.NewBuiltin("verify_blockchain_proof", env.builtinVerifyProof),
		"lookup_map":              starlark.NewBuiltin("lookup_map", env.builtinLookupMap),

		// Matching Actions (Sieve parity)
		"address_matches": starlark.NewBuiltin("address_matches", env.builtinAddressMatches),
		"regex_match":     starlark.NewBuiltin("regex_match", env.builtinRegexMatch),

		// Execution Actions (Mutates the env.Action state)
		"accept":      starlark.NewBuiltin("accept", env.builtinAccept),
		"discard":     starlark.NewBuiltin("discard", env.builtinDiscard),
		"reject":      starlark.NewBuiltin("reject", env.builtinReject),
		"fileinto":    starlark.NewBuiltin("fileinto", env.builtinFileinto),
		"redirect":    starlark.NewBuiltin("redirect", env.builtinRedirect),
		"vacation":    starlark.NewBuiltin("vacation", env.builtinVacation),
		"add_flag":    starlark.NewBuiltin("add_flag", env.builtinAddFlag),
		"remove_flag": starlark.NewBuiltin("remove_flag", env.builtinRemoveFlag),
	}
}

// ---------------------------------------------------------
// Context Readers
// ---------------------------------------------------------

func (e *ExecutionEnv) builtinGetHeader(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var headerName, defaultVal string
	if err := starlark.UnpackArgs("get_header", args, kwargs, "name", &headerName, "default?", &defaultVal); err != nil {
		return starlark.None, err
	}

	// AMP unencrypted headers aren't standard RFC822 strings, but the payload inside is.
	// For simulation, we'll pretend the payload headers map exists on the envelope
	if e.Message != nil && e.Message.Headers != nil {
		// Example extraction logic
		// In production, we'd inspect the decrypted AMFPayload.extended_headers
		val := "" // e.Message.Payload.ExtendedHeaders[headerName] if decrypted early
		if val == "" {
			return starlark.String(defaultVal), nil
		}
		return starlark.String(val), nil
	}

	return starlark.String(defaultVal), nil
}

func (e *ExecutionEnv) builtinGetSenderDid(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if e.Message != nil && e.Message.Headers != nil {
		return starlark.String(e.Message.Headers.SenderDid), nil
	}
	return starlark.String(""), nil
}

func (e *ExecutionEnv) builtinGetRecipientDid(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if e.Message != nil && e.Message.Headers != nil {
		return starlark.String(e.Message.Headers.RecipientDid), nil
	}
	return starlark.String(""), nil
}

func (e *ExecutionEnv) builtinVerifyProof(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if e.Message != nil {
		return starlark.Bool(e.Message.BlockchainProof != ""), nil
	}
	return starlark.Bool(false), nil
}

func (e *ExecutionEnv) builtinLookupMap(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var mapName, key string
	if err := starlark.UnpackArgs("lookup_map", args, kwargs, "map_name", &mapName, "key", &key); err != nil {
		return starlark.None, err
	}

	if e.MapEngine == nil {
		return starlark.String(""), nil
	}

	// Route based on map name to SQLite
	switch mapName {
	case "access_map":
		val, _ := e.MapEngine.CheckAccessMap(key)
		return starlark.String(val), nil
	case "mx_record":
		val, _ := e.MapEngine.LookupMXRecord(key)
		return starlark.String(val), nil
	case "virtual_alias":
		vals, _ := e.MapEngine.ExpandVirtualAlias(key)
		if len(vals) > 0 {
			return starlark.String(vals[0]), nil // simplified returning single string for simplicity
		}
	}

	return starlark.String(""), nil
}

// ---------------------------------------------------------
// Matchers
// ---------------------------------------------------------

func (e *ExecutionEnv) builtinAddressMatches(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var target, substr string
	if err := starlark.UnpackArgs("address_matches", args, kwargs, "target", &target, "substr", &substr); err != nil {
		return starlark.None, err
	}
	return starlark.Bool(strings.Contains(strings.ToLower(target), strings.ToLower(substr))), nil
}

func (e *ExecutionEnv) builtinRegexMatch(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, input string
	if err := starlark.UnpackArgs("regex_match", args, kwargs, "pattern", &pattern, "input", &input); err != nil {
		return starlark.None, err
	}

	matched, err := regexp.MatchString(pattern, input)
	if err != nil {
		return starlark.Bool(false), nil // fail safely
	}
	return starlark.Bool(matched), nil
}

// ---------------------------------------------------------
// Action Mutators
// ---------------------------------------------------------

func (e *ExecutionEnv) builtinAccept(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	e.Action.Type = ActionAccept
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinDiscard(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	e.Action.Type = ActionDiscard
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinReject(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg string
	if err := starlark.UnpackArgs("reject", args, kwargs, "message", &msg); err != nil {
		return starlark.None, err
	}
	e.Action.Type = ActionReject
	e.Action.Arg = msg
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinFileinto(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var folder string
	if err := starlark.UnpackArgs("fileinto", args, kwargs, "folder", &folder); err != nil {
		return starlark.None, err
	}
	e.Action.Type = ActionFileinto
	e.Action.Arg = folder
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinRedirect(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var dest string
	if err := starlark.UnpackArgs("redirect", args, kwargs, "destination", &dest); err != nil {
		return starlark.None, err
	}
	e.Action.Type = ActionRedirect
	e.Action.Arg = dest
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinVacation(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var days int
	var body string
	if err := starlark.UnpackArgs("vacation", args, kwargs, "days", &days, "body", &body); err != nil {
		return starlark.None, err
	}
	e.Action.Type = ActionVacation
	e.Action.Days = days
	e.Action.Body = body
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinAddFlag(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var flag string
	if err := starlark.UnpackArgs("add_flag", args, kwargs, "flag", &flag); err != nil {
		return starlark.None, err
	}
	e.Action.AddFlags = append(e.Action.AddFlags, flag)
	return starlark.None, nil
}

func (e *ExecutionEnv) builtinRemoveFlag(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var flag string
	if err := starlark.UnpackArgs("remove_flag", args, kwargs, "flag", &flag); err != nil {
		return starlark.None, err
	}
	e.Action.RemFlags = append(e.Action.RemFlags, flag)
	return starlark.None, nil
}
