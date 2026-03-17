package policy

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	
	"go.starlark.net/starlark"
)

// CreateStarlarkBuiltins creates all email-specific built-in functions for Starlark (exported)
func CreateStarlarkBuiltins(emailCtx *EmailContext) starlark.StringDict {
	return createStarlarkBuiltins(emailCtx)
}

// ResetGlobalAction resets the global action state (exported)
func ResetGlobalAction() {
	globalAction = nil
	globalHeaders = nil
}

// GetGlobalAction returns the current global action (exported)
func GetGlobalAction() *Action {
	return globalAction
}

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

		// === MailScript Extensions ===
		// Content search
		"search_body":   starlark.NewBuiltin("search_body", makeSearchBody(emailCtx)),
		"regex_match":   starlark.NewBuiltin("regex_match", makeRegexMatch()),

		// Message metadata
		"getmimetype":    starlark.NewBuiltin("getmimetype", makeGetMimeType(emailCtx)),
		"getspamscore":   starlark.NewBuiltin("getspamscore", makeGetSpamScore(emailCtx)),
		"getvirusstatus": starlark.NewBuiltin("getvirusstatus", makeGetVirusStatus(emailCtx)),
		"body_size":      starlark.NewBuiltin("body_size", makeBodySize(emailCtx)),
		"header_size":    starlark.NewBuiltin("header_size", makeHeaderSize(emailCtx)),
		"num_envelope":   starlark.NewBuiltin("num_envelope", makeNumEnvelope(emailCtx)),
		"get_recipient_did": starlark.NewBuiltin("get_recipient_did", makeGetRecipientDID(emailCtx)),

		// Additional actions
		"quarantine":        starlark.NewBuiltin("quarantine", makeQuarantine()),
		"drop":              starlark.NewBuiltin("drop", makeDrop()),
		"bounce":            starlark.NewBuiltin("bounce", makeBounce()),
		"auto_reply":        starlark.NewBuiltin("auto_reply", makeAutoReply()),
		"add_to_next_digest": starlark.NewBuiltin("add_to_next_digest", makeAddToNextDigest()),

		// SMTP responses
		"reply_with_smtp_error": starlark.NewBuiltin("reply_with_smtp_error", makeReplyWithSMTPError()),
		"reply_with_smtp_dsn":   starlark.NewBuiltin("reply_with_smtp_dsn", makeReplyWithSMTPDSN()),

		// Routing
		"divert_to":        starlark.NewBuiltin("divert_to", makeDivertTo()),
		"screen_to":        starlark.NewBuiltin("screen_to", makeScreenTo()),
		"force_second_pass": starlark.NewBuiltin("force_second_pass", makeForceSecondPass()),

		// Security controls
		"skip_malware_check":   starlark.NewBuiltin("skip_malware_check", makeSkipMalwareCheck()),
		"skip_spam_check":      starlark.NewBuiltin("skip_spam_check", makeSkipSpamCheck()),
		"skip_whitelist_check": starlark.NewBuiltin("skip_whitelist_check", makeSkipWhitelistCheck()),
		"set_dlp":              starlark.NewBuiltin("set_dlp", makeSetDLP()),
		"skip_dlp":             starlark.NewBuiltin("skip_dlp", makeSkipDLP()),

		// Logging
		"log_entry": starlark.NewBuiltin("log_entry", makeLogEntry()),

		// Content filtering
		"get_content_filter":       starlark.NewBuiltin("get_content_filter", makeGetContentFilter(emailCtx)),
		"get_content_filter_name":  starlark.NewBuiltin("get_content_filter_name", makeGetContentFilterName(emailCtx)),
		"get_content_filter_rules": starlark.NewBuiltin("get_content_filter_rules", makeGetContentFilterRules(emailCtx)),
		"set_content_filter_rules": starlark.NewBuiltin("set_content_filter_rules", makeSetContentFilterRules()),

		// Instance information
		"get_instance":      starlark.NewBuiltin("get_instance", makeGetInstance(emailCtx)),
		"get_instance_name": starlark.NewBuiltin("get_instance_name", makeGetInstanceName(emailCtx)),

		// DNS and network
		"get_sender_ip":     starlark.NewBuiltin("get_sender_ip", makeGetSenderIP(emailCtx)),
		"get_sender_domain": starlark.NewBuiltin("get_sender_domain", makeGetSenderDomain(emailCtx)),
		"dns_check":         starlark.NewBuiltin("dns_check", makeDNSCheck()),
		"dns_resolution":    starlark.NewBuiltin("dns_resolution", makeDNSResolution()),
		"domain_resolution": starlark.NewBuiltin("domain_resolution", makeDomainResolution()),
		"rbl_check":         starlark.NewBuiltin("rbl_check", makeRBLCheck(emailCtx)),
		"get_rbl_status":    starlark.NewBuiltin("get_rbl_status", makeGetRBLStatus(emailCtx)),
		"valid_mx":          starlark.NewBuiltin("valid_mx", makeValidMX()),
		"get_mx_records":    starlark.NewBuiltin("get_mx_records", makeGetMXRecords()),
		"mx_in_rbl":         starlark.NewBuiltin("mx_in_rbl", makeMXInRBL()),
		"is_mx_ipv4":        starlark.NewBuiltin("is_mx_ipv4", makeIsMXIPv4()),
		"is_mx_ipv6":        starlark.NewBuiltin("is_mx_ipv6", makeIsMXIPv6()),

		// Received headers analysis
		"check_received_header": starlark.NewBuiltin("check_received_header", makeCheckReceivedHeader(emailCtx)),
		"get_received_headers":  starlark.NewBuiltin("get_received_headers", makeGetReceivedHeaders(emailCtx)),
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

// === MailScript Extension Functions ===

// Content Search Functions

func makeSearchBody(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var text string
		if err := starlark.UnpackArgs("search_body", args, kwargs, "text", &text); err != nil {
			return nil, err
		}
		found := strings.Contains(ctx.BodyText, text) || strings.Contains(string(ctx.Body), text)
		return starlark.Bool(found), nil
	}
}

func makeRegexMatch() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var pattern, text string
		if err := starlark.UnpackArgs("regex_match", args, kwargs, "pattern", &pattern, "text", &text); err != nil {
			return nil, err
		}
		matched, err := regexp.MatchString(pattern, text)
		if err != nil {
			return starlark.Bool(false), nil
		}
		return starlark.Bool(matched), nil
	}
}

// Message Metadata Functions

func makeGetMimeType(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("getmimetype", args, kwargs); err != nil {
			return nil, err
		}
		if ctx.MimeType == "" {
			// Try to get from Content-Type header
			ctx.MimeType = ctx.GetHeader("Content-Type")
		}
		return starlark.String(ctx.MimeType), nil
	}
}

func makeGetSpamScore(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("getspamscore", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.Float(ctx.SpamScore), nil
	}
}

func makeGetVirusStatus(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("getvirusstatus", args, kwargs); err != nil {
			return nil, err
		}
		if ctx.VirusStatus == "" {
			ctx.VirusStatus = "unknown"
		}
		return starlark.String(ctx.VirusStatus), nil
	}
}

func makeBodySize(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("body_size", args, kwargs); err != nil {
			return nil, err
		}
		if ctx.BodySize == 0 {
			ctx.BodySize = int64(len(ctx.Body))
		}
		return starlark.MakeInt64(ctx.BodySize), nil
	}
}

func makeHeaderSize(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("header_size", args, kwargs); err != nil {
			return nil, err
		}
		if ctx.HeaderSize == 0 {
			// Calculate header size
			headerSize := 0
			for k, values := range ctx.Headers {
				for _, v := range values {
					headerSize += len(k) + len(v) + 4 // +4 for ": " and "\r\n"
				}
			}
			ctx.HeaderSize = int64(headerSize)
		}
		return starlark.MakeInt64(ctx.HeaderSize), nil
	}
}

func makeNumEnvelope(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("num_envelope", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.MakeInt(len(ctx.EnvelopeSenders)), nil
	}
}

func makeGetRecipientDID(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_recipient_did", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.RecipientDID), nil
	}
}

// Action Functions

func makeQuarantine() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("quarantine", args, kwargs); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:    ActionQuarantine,
			Headers: globalHeaders,
		}
		return starlark.None, nil
	}
}

func makeDrop() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("drop", args, kwargs); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type: ActionDrop,
		}
		return starlark.None, nil
	}
}

func makeBounce() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("bounce", args, kwargs); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type: ActionBounce,
		}
		return starlark.None, nil
	}
}

func makeAutoReply() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var text string
		if err := starlark.UnpackArgs("auto_reply", args, kwargs, "text", &text); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:          ActionAutoReply,
			AutoReplyText: text,
		}
		return starlark.None, nil
	}
}

func makeAddToNextDigest() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("add_to_next_digest", args, kwargs); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type: ActionAddToDigest,
		}
		return starlark.Bool(true), nil
	}
}

// SMTP Response Functions

func makeReplyWithSMTPError() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var code int
		if err := starlark.UnpackArgs("reply_with_smtp_error", args, kwargs, "code", &code); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:     ActionSMTPError,
			SMTPCode: code,
		}
		return starlark.None, nil
	}
}

func makeReplyWithSMTPDSN() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var dsn string
		if err := starlark.UnpackArgs("reply_with_smtp_dsn", args, kwargs, "dsn", &dsn); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:    ActionSMTPDSN,
			SMTPDSN: dsn,
		}
		return starlark.None, nil
	}
}

// Routing Functions

func makeDivertTo() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var emailAddress string
		if err := starlark.UnpackArgs("divert_to", args, kwargs, "email_address", &emailAddress); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:   ActionDivertTo,
			Target: emailAddress,
		}
		return starlark.None, nil
	}
}

func makeScreenTo() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var emailAddress string
		if err := starlark.UnpackArgs("screen_to", args, kwargs, "email_address", &emailAddress); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:   ActionScreenTo,
			Target: emailAddress,
		}
		return starlark.None, nil
	}
}

func makeForceSecondPass() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var mailserver string
		if err := starlark.UnpackArgs("force_second_pass", args, kwargs, "mailserver", &mailserver); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:              ActionForceSecondPass,
			ForceSecondServer: mailserver,
		}
		return starlark.None, nil
	}
}

// Security Control Functions

func makeSkipMalwareCheck() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var sender string
		if err := starlark.UnpackArgs("skip_malware_check", args, kwargs, "sender", &sender); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:        ActionSkipCheck,
			CheckToSkip: "malware",
			Target:      sender,
		}
		return starlark.None, nil
	}
}

func makeSkipSpamCheck() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var sender string
		if err := starlark.UnpackArgs("skip_spam_check", args, kwargs, "sender", &sender); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:        ActionSkipCheck,
			CheckToSkip: "spam",
			Target:      sender,
		}
		return starlark.None, nil
	}
}

func makeSkipWhitelistCheck() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var ip string
		if err := starlark.UnpackArgs("skip_whitelist_check", args, kwargs, "ip", &ip); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:        ActionSkipCheck,
			CheckToSkip: "whitelist",
			Target:      ip,
		}
		return starlark.None, nil
	}
}

func makeSetDLP() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var mode, target string
		if err := starlark.UnpackArgs("set_dlp", args, kwargs, "mode", &mode, "target", &target); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:      ActionSetDLP,
			DLPMode:   mode,
			DLPTarget: target,
		}
		return starlark.None, nil
	}
}

func makeSkipDLP() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var mode, target string
		if err := starlark.UnpackArgs("skip_dlp", args, kwargs, "mode", &mode, "target", &target); err != nil {
			return nil, err
		}
		globalAction = &Action{
			Type:      ActionSetDLP,
			DLPMode:   "skip_" + mode,
			DLPTarget: target,
		}
		return starlark.None, nil
	}
}

// Logging Functions

func makeLogEntry() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var message string
		if err := starlark.UnpackArgs("log_entry", args, kwargs, "message", &message); err != nil {
			return nil, err
		}
		fmt.Printf("[MAILSCRIPT] %s\n", message)
		return starlark.None, nil
	}
}

// Content Filter Functions

func makeGetContentFilter(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_content_filter", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.ContentFilter), nil
	}
}

func makeGetContentFilterName(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_content_filter_name", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.ContentFilterName), nil
	}
}

func makeGetContentFilterRules(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_content_filter_rules", args, kwargs); err != nil {
			return nil, err
		}
		// Return empty dict for now - implementation would load actual rules
		return starlark.NewDict(0), nil
	}
}

func makeSetContentFilterRules() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var rule string
		if err := starlark.UnpackArgs("set_content_filter_rules", args, kwargs, "rule", &rule); err != nil {
			return nil, err
		}
		// TODO: Implement actual filter rule setting
		return starlark.Bool(true), nil
	}
}

// Instance Functions

func makeGetInstance(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_instance", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.Instance), nil
	}
}

func makeGetInstanceName(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_instance_name", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.InstanceName), nil
	}
}

// DNS and Network Functions

func makeGetSenderIP(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_sender_ip", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.RemoteIP), nil
	}
}

func makeGetSenderDomain(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_sender_domain", args, kwargs); err != nil {
			return nil, err
		}
		return starlark.String(ctx.GetFromDomain()), nil
	}
}

func makeDNSCheck() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("dns_check", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		_, err := net.LookupHost(domain)
		return starlark.Bool(err == nil), nil
	}
}

func makeDNSResolution() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("dns_resolution", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		addrs, err := net.LookupHost(domain)
		if err != nil || len(addrs) == 0 {
			return starlark.String(""), nil
		}
		return starlark.String(addrs[0]), nil
	}
}

func makeDomainResolution() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var sender string
		var verify bool
		if err := starlark.UnpackArgs("domain_resolution", args, kwargs, "sender", &sender, "verify", &verify); err != nil {
			return nil, err
		}
		// Extract domain from sender email
		parts := strings.Split(sender, "@")
		if len(parts) != 2 {
			return starlark.Bool(false), nil
		}
		domain := parts[1]
		_, err := net.LookupHost(domain)
		return starlark.Bool(err == nil), nil
	}
}

func makeRBLCheck(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var ip, rblServer string
		if err := starlark.UnpackArgs("rbl_check", args, kwargs, "ip", &ip, "rbl_server?", &rblServer); err != nil {
			return nil, err
		}
		if rblServer == "" {
			rblServer = "zen.spamhaus.org" // Default RBL
		}

		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			return starlark.Bool(false), nil
		}

		reversed := reverseIP(parsedIP)
		query := fmt.Sprintf("%s.%s", reversed, rblServer)
		addrs, err := net.LookupHost(query)
		return starlark.Bool(err == nil && len(addrs) > 0), nil
	}
}

func makeGetRBLStatus(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_rbl_status", args, kwargs); err != nil {
			return nil, err
		}
		dict := starlark.NewDict(2)
		listed := len(ctx.RBLResults) > 0 && ctx.RBLResults[0].Listed
		rblName := ""
		if listed {
			rblName = ctx.RBLResults[0].Server
		}
		dict.SetKey(starlark.String("listed"), starlark.Bool(listed))
		dict.SetKey(starlark.String("rbl_name"), starlark.String(rblName))
		return dict, nil
	}
}

func makeValidMX() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("valid_mx", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		mxRecords, err := net.LookupMX(domain)
		return starlark.Bool(err == nil && len(mxRecords) > 0), nil
	}
}

func makeGetMXRecords() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("get_mx_records", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		mxRecords, err := net.LookupMX(domain)
		if err != nil {
			return starlark.NewList(nil), nil
		}
		list := make([]starlark.Value, len(mxRecords))
		for i, mx := range mxRecords {
			list[i] = starlark.String(mx.Host)
		}
		return starlark.NewList(list), nil
	}
}

func makeMXInRBL() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain, rblServer string
		if err := starlark.UnpackArgs("mx_in_rbl", args, kwargs, "domain", &domain, "rbl_server?", &rblServer); err != nil {
			return nil, err
		}
		if rblServer == "" {
			rblServer = "zen.spamhaus.org"
		}

		mxRecords, err := net.LookupMX(domain)
		if err != nil {
			return starlark.Bool(false), nil
		}

		for _, mx := range mxRecords {
			addrs, err := net.LookupHost(mx.Host)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ip := net.ParseIP(addr)
				if ip == nil {
					continue
				}
				reversed := reverseIP(ip)
				query := fmt.Sprintf("%s.%s", reversed, rblServer)
				_, err := net.LookupHost(query)
				if err == nil {
					return starlark.Bool(true), nil
				}
			}
		}
		return starlark.Bool(false), nil
	}
}

func makeIsMXIPv4() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("is_mx_ipv4", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		mxRecords, err := net.LookupMX(domain)
		if err != nil || len(mxRecords) == 0 {
			return starlark.Bool(false), nil
		}

		for _, mx := range mxRecords {
			addrs, err := net.LookupHost(mx.Host)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ip := net.ParseIP(addr)
				if ip != nil && ip.To4() != nil {
					return starlark.Bool(true), nil
				}
			}
		}
		return starlark.Bool(false), nil
	}
}

func makeIsMXIPv6() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var domain string
		if err := starlark.UnpackArgs("is_mx_ipv6", args, kwargs, "domain", &domain); err != nil {
			return nil, err
		}
		mxRecords, err := net.LookupMX(domain)
		if err != nil || len(mxRecords) == 0 {
			return starlark.Bool(false), nil
		}

		for _, mx := range mxRecords {
			addrs, err := net.LookupHost(mx.Host)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ip := net.ParseIP(addr)
				if ip != nil && ip.To4() == nil {
					return starlark.Bool(true), nil
				}
			}
		}
		return starlark.Bool(false), nil
	}
}

// Received Headers Functions

func makeCheckReceivedHeader(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var level int
		if err := starlark.UnpackArgs("check_received_header", args, kwargs, "level", &level); err != nil {
			return nil, err
		}
		if level >= 0 && level < len(ctx.ReceivedHeaders) {
			return starlark.String(ctx.ReceivedHeaders[level]), nil
		}
		return starlark.String(""), nil
	}
}

func makeGetReceivedHeaders(ctx *EmailContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs("get_received_headers", args, kwargs); err != nil {
			return nil, err
		}
		headers := make([]starlark.Value, len(ctx.ReceivedHeaders))
		for i, hdr := range ctx.ReceivedHeaders {
			headers[i] = starlark.String(hdr)
		}
		return starlark.NewList(headers), nil
	}
}
