package policy

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	
	"go.starlark.net/starlark"
)

// createStarlarkBuiltins creates all email-specific built-in functions for Starlark
func createStarlarkBuiltins(emailCtx *EmailContext) starlark.StringDict {
	return starlark.StringDict{
		// === Email Inspection ===
		"has_header":      starlark.NewBuiltin("has_header", makeHasHeader(emailCtx)),
		"get_header":      starlark.NewBuiltin("get_header", makeGetHeader(emailCtx)),
		"get_all_headers": starlark.NewBuiltin("get_all_headers", makeGetAllHeaders(emailCtx)),
		"get_body":        starlark.NewBuiltin("get_body", makeGetBody(emailCtx)),
		"get_attachments": starlark.NewBuiltin("get_attachments", makeGetAttachments(emailCtx)),

		// === Envelope ===
		"get_from":      starlark.NewBuiltin("get_from", makeGetFrom(emailCtx)),
		"get_to":        starlark.NewBuiltin("get_to", makeGetTo(emailCtx)),
		"get_remote_ip": starlark.NewBuiltin("get_remote_ip", makeGetRemoteIP(emailCtx)),

		// === Security Checks ===
		"check_spf":   starlark.NewBuiltin("check_spf", makeCheckSPF(emailCtx)),
		"check_dkim":  starlark.NewBuiltin("check_dkim", makeCheckDKIM(emailCtx)),
		"check_dmarc": starlark.NewBuiltin("check_dmarc", makeCheckDMARC(emailCtx)),
		"check_rbl":   starlark.NewBuiltin("check_rbl", makeCheckRBL(emailCtx)),
		"get_ip_reputation": starlark.NewBuiltin("get_ip_reputation", makeGetIPReputation(emailCtx)),

		// === Actions ===
		"accept":       starlark.NewBuiltin("accept", makeAccept()),
		"reject":       starlark.NewBuiltin("reject", makeReject()),
		"defer":        starlark.NewBuiltin("defer", makeDefer()),
		"discard":      starlark.NewBuiltin("discard", makeDiscard()),
		"redirect":     starlark.NewBuiltin("redirect", makeRedirect()),
		"fileinto":     starlark.NewBuiltin("fileinto", makeFileinto()),
		"add_header":   starlark.NewBuiltin("add_header", makeAddHeader()),
		"remove_header": starlark.NewBuiltin("remove_header", makeRemoveHeader()),

		// === Utilities ===
		"match_pattern": starlark.NewBuiltin("match_pattern", makeMatchPattern()),
		"lookup_dns":    starlark.NewBuiltin("lookup_dns", makeLookupDNS()),
		"is_in_group":   starlark.NewBuiltin("is_in_group", makeIsInGroup(emailCtx)),
		"log":           starlark.NewBuiltin("log", makeLog()),
		"notify":        starlark.NewBuiltin("notify", makeNotify()),
	}
}

// === Email Inspection Functions ===

func makeHasHeader(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string
		if err := starlark.UnpackArgs("has_header", args, kwargs, "name", &name); err != nil {
			return nil, err
		}
		return starlark.Bool(ctx.HasHeader(name)), nil
	}
}

func makeGetHeader(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string
		if err := starlark.UnpackArgs("get_header", args, kwargs, "name", &name); err != nil {
			return nil, err
		}
		return starlark.String(ctx.GetHeader(name)), nil
	}
}

func makeGetAllHeaders(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string
		if err := starlark.UnpackArgs("get_all_headers", args, kwargs, "name", &name); err != nil {
			return nil, err
		}
		values := ctx.GetAllHeaders(name)
		list := make([]starlark.Value, len(values))
		for i, v := range values {
			list[i] = starlark.String(v)
		}
		return starlark.NewList(list), nil
	}
}

func makeGetBody(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_body", args, kwargs); err != nil {
			return nil, err
		}
		// Return body text if available, otherwise full body
		if ctx.BodyText != "" {
			return starlark.String(ctx.BodyText), nil
		}
		return starlark.String(string(ctx.Body)), nil
	}
}

func makeGetAttachments(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_attachments", args, kwargs); err != nil {
			return nil, err
		}

		list := make([]starlark.Value, len(ctx.Attachments))
		for i, att := range ctx.Attachments {
			list[i] = &starlarkAttachment{att: att}
		}
		return starlark.NewList(list), nil
	}
}

// === Envelope Functions ===

func makeGetFrom(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_from", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.From), nil
	}
}

func makeGetTo(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_to", args, kwargs); err != nil {
			return nil, err
		}
		list := make([]starlark.Value, len(ctx.To))
		for i, v := range ctx.To {
			list[i] = starlark.String(v)
		}
		return starlark.NewList(list), nil
	}
}

func makeGetRemoteIP(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_remote_ip", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.RemoteIP), nil
	}
}

// === Security Check Functions ===

func makeCheckSPF(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("check_spf", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(string(ctx.SPFResult)), nil
	}
}

func makeCheckDKIM(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("check_dkim", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(string(ctx.DKIMResult)), nil
	}
}

func makeCheckDMARC(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("check_dmarc", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(string(ctx.DMARCResult)), nil
	}
}

func makeCheckRBL(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var server string
		if err := starlark.UnpackArgs("check_rbl", args, kwargs, "server", &server); err != nil {
			return nil, err
		}

		// Check existing RBL results
		for _, rbl := range ctx.RBLResults {
			if rbl.Server == server {
				return starlark.Bool(rbl.Listed), nil
			}
		}

		// If not in cache, do live lookup
		ip := net.ParseIP(ctx.RemoteIP)
		if ip == nil {
			return starlark.Bool(false), nil
		}

		// Reverse IP for RBL query
		reversed := reverseIP(ip)
		query := fmt.Sprintf("%s.%s", reversed, server)

		// Simple DNS lookup to check if listed
		addrs, err := net.LookupHost(query)
		listed := err == nil && len(addrs) > 0

		return starlark.Bool(listed), nil
	}
}

func makeGetIPReputation(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_ip_reputation", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.MakeInt(ctx.IPReputation.Score), nil
	}
}

// === Action Functions ===

var globalAction *Action
var globalHeaders []Header

func makeAccept() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var reason string
		if err := starlark.UnpackArgs("accept", args, kwargs, "reason?", &reason); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:    ActionAccept,
			Reason:  reason,
			Headers: globalHeaders,
		}
		return starlark.None, nil
	}
}

func makeReject() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var reason string
		if err := starlark.UnpackArgs("reject", args, kwargs, "reason", &reason); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:   ActionReject,
			Reason: reason,
		}
		return starlark.None, nil
	}
}

func makeDefer() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var reason string
		var retryAfter int = 300 // Default 5 minutes
		if err := starlark.UnpackArgs("defer", args, kwargs, "reason", &reason, "retry_after?", &retryAfter); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:       ActionDefer,
			Reason:     reason,
			RetryAfter: retryAfter,
		}
		return starlark.None, nil
	}
}

func makeDiscard() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var reason string
		if err := starlark.UnpackArgs("discard", args, kwargs, "reason?", &reason); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:   ActionDiscard,
			Reason: reason,
		}
		return starlark.None, nil
	}
}

func makeRedirect() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var target string
		if err := starlark.UnpackArgs("redirect", args, kwargs, "target", &target); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:   ActionRedirect,
			Target: target,
		}
		return starlark.None, nil
	}
}

func makeFileinto() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var folder string
		if err := starlark.UnpackArgs("fileinto", args, kwargs, "folder", &folder); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:    ActionFileinto,
			Target:  folder,
			Headers: globalHeaders,
		}
		return starlark.None, nil
	}
}

func makeAddHeader() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name, value string
		if err := starlark.UnpackArgs("add_header", args, kwargs, "name", &name, "value", &value); err != nil {
			return nil, err
		}
		globalHeaders = append(globalHeaders, Header{
			Name:   name,
			Value:  value,
			Action: "add",
		})
		return starlark.None, nil
	}
}

func makeRemoveHeader() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string
		if err := starlark.UnpackArgs("remove_header", args, kwargs, "name", &name); err != nil {
			return nil, err
		}
		globalHeaders = append(globalHeaders, Header{
			Name:   name,
			Action: "remove",
		})
		return starlark.None, nil
	}
}

// === Utility Functions ===

func makeMatchPattern() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var text, pattern string
		if err := starlark.UnpackArgs("match_pattern", args, kwargs, "text", &text, "pattern", &pattern); err != nil {
			return nil, err
		}

		matched, err := regexp.MatchString(pattern, text)
		if err != nil {
			return starlark.Bool(false), nil
		}
		return starlark.Bool(matched), nil
	}
}

func makeLookupDNS() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain, recordType string
		if err := starlark.UnpackArgs("lookup_dns", args, kwargs, "domain", &domain, "type", &recordType); err != nil {
			return nil, err
		}

		// Simple implementation - only support A records for now
		if recordType == "A" || recordType == "a" {
			addrs, err := net.LookupHost(domain)
			if err != nil {
				return starlark.NewList(nil), nil
			}
			list := make([]starlark.Value, len(addrs))
			for i, addr := range addrs {
				list[i] = starlark.String(addr)
			}
			return starlark.NewList(list), nil
		}

		return starlark.NewList(nil), nil
	}
}

func makeIsInGroup(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var email, group string
		if err := starlark.UnpackArgs("is_in_group", args, kwargs, "email", &email, "group", &group); err != nil {
			return nil, err
		}

		// Check sender groups
		for _, g := range ctx.SenderGroups {
			if g == group && email == ctx.From {
				return starlark.Bool(true), nil
			}
		}

		// Check recipient groups
		for _, g := range ctx.RecipientGroups {
			if g == group {
				for _, recipient := range ctx.To {
					if recipient == email {
						return starlark.Bool(true), nil
					}
				}
			}
		}

		return starlark.Bool(false), nil
	}
}

func makeLog() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var level, message string
		if err := starlark.UnpackArgs("log", args, kwargs, "level", &level, "message", &message); err != nil {
			return nil, err
		}
		// TODO: Integrate with actual logger
		fmt.Printf("[POLICY-%s] %s\n", strings.ToUpper(level), message)
		return starlark.None, nil
	}
}

func makeNotify() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var target, message string
		if err := starlark.UnpackArgs("notify", args, kwargs, "target", &target, "message", &message); err != nil {
			return nil, err
		}

		// Store notification in action
		if globalAction == nil {
			globalAction = &Action{Type: ActionKeep}
		}
		globalAction.Notify = &Notify{
			Method:  "mailto",
			Target:  target,
			Message: message,
		}
		return starlark.None, nil
	}
}

// === Helper Types ===

// starlarkAttachment wraps a Attachment for Starlark
type starlarkAttachment struct {
	att Attachment
}

func (a *starlarkAttachment) String() string {
	return fmt.Sprintf("Attachment(%s)", a.att.Filename)
}

func (a *starlarkAttachment) Type() string {
	return "Attachment"
}

func (a *starlarkAttachment) Freeze() {}

func (a *starlarkAttachment) Truth() starlark.Bool {
	return true
}

func (a *starlarkAttachment) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: Attachment")
}

func (a *starlarkAttachment) Attr(name string) (starlark.Value, error) {
	switch name {
	case "filename":
		return starlark.String(a.att.Filename), nil
	case "content_type":
		return starlark.String(a.att.ContentType), nil
	case "size":
		return starlark.MakeInt64(a.att.Size), nil
	case "extension":
		return starlark.String(a.att.Extension), nil
	}
	return nil, nil
}

func (a *starlarkAttachment) AttrNames() []string {
	return []string{"filename", "content_type", "size", "extension"}
}

// reverseIP reverses an IP address for RBL lookup
func reverseIP(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", ip4[3], ip4[2], ip4[1], ip4[0])
	}
	// IPv6 not implemented yet
	return ""
}
