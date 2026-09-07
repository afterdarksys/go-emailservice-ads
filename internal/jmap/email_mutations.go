package jmap

import (
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"strings"
)

func (j *JMAPServer) emailMutations() bool { _, ok := j.store.(mailstate.EmailMutator); return ok }

func emailPatch(value interface{}) (mailstate.EmailPatch, bool) {
	p := mailstate.EmailPatch{}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return p, false
	}
	keywords := map[string]interface{}{}
	for key, v := range obj {
		switch {
		case key == "mailboxIds":
			members, ok := v.(map[string]interface{})
			if !ok {
				return p, false
			}
			ids := []string{}
			for mid, v := range members {
				if mid == "" || v != true {
					return p, false
				}
				ids = append(ids, mid)
			}
			p.Mailboxes = &ids
		case strings.HasPrefix(key, "mailboxIds/"):
			mid := strings.TrimPrefix(key, "mailboxIds/")
			// Current mailbox IDs are opaque hexadecimal strings, with no path escapes.
			if mid == "" || strings.ContainsAny(mid, "/~") {
				return p, false
			}
			if v == nil {
				p.RemoveMailboxes = append(p.RemoveMailboxes, mid)
			} else if v == true {
				p.AddMailboxes = append(p.AddMailboxes, mid)
			} else {
				return p, false
			}
		default:
			keywords[key] = v
		}
	}
	if p.Mailboxes != nil && (len(p.AddMailboxes) > 0 || len(p.RemoveMailboxes) > 0) {
		return p, false
	}
	p.Keywords, ok = keywordPatch(keywords)
	return p, ok
}

func (j *JMAPServer) mutateEmails(ctx context.Context, user, since string, update, create map[string]interface{}, destroy []string, id string) MethodResponse {
	patches := map[string]mailstate.EmailPatch{}
	nu, nc, nd := map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}
	doomed := map[string]bool{}
	for _, mid := range destroy {
		doomed[mid] = true
	}
	for mid, v := range update {
		if doomed[mid] {
			nu[mid] = map[string]interface{}{"type": "willDestroy"}
			continue
		}
		p, ok := emailPatch(v)
		if !ok {
			nu[mid] = map[string]interface{}{"type": "invalidProperties", "description": "Only keywords and mailboxIds updates are supported"}
		} else {
			patches[mid] = p
		}
	}
	for key := range create {
		nc[key] = map[string]interface{}{"type": "forbidden", "description": "Email creation is not supported"}
	}
	r, err := j.store.(mailstate.EmailMutator).SetEmails(ctx, user, since, patches, destroy)
	if errors.Is(err, mailstate.ErrStateMismatch) {
		return methodError("stateMismatch", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	updated := map[string]interface{}{}
	for _, mid := range r.Updated {
		updated[mid] = nil
	}
	for mid, kind := range r.NotUpdated {
		nu[mid] = map[string]interface{}{"type": kind}
	}
	for mid, kind := range r.NotDestroyed {
		nd[mid] = map[string]interface{}{"type": kind}
	}
	return MethodResponse{Name: "Email/set", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": r.OldState, "newState": r.NewState, "created": map[string]interface{}{}, "updated": updated, "destroyed": r.Destroyed, "notCreated": nc, "notUpdated": nu, "notDestroyed": nd}}
}
