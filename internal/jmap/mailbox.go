package jmap

import (
	"context"
	"errors"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"sort"
	"strings"
)

func (j *JMAPServer) mailboxWrites() bool { _, ok := j.store.(mailstate.MailboxStore); return ok }
func (j *JMAPServer) durableMailboxGet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	for key := range args {
		if key != "accountId" && key != "ids" && key != "properties" {
			return methodError("invalidArguments", id)
		}
	}
	wanted := map[string]bool{}
	explicit := args["ids"] != nil
	if explicit {
		ids := toStringSlice(args["ids"])
		if ids == nil {
			return methodError("invalidArguments", id)
		}
		if len(ids) > maxJMAPObjects {
			return methodError("tooManyObjectsInGet", id)
		}
		for _, v := range ids {
			wanted[v] = true
		}
	}
	properties := map[string]bool{"id": true}
	project := args["properties"] != nil
	if project {
		names := toStringSlice(args["properties"])
		if names == nil {
			return methodError("invalidArguments", id)
		}
		for _, p := range names {
			switch p {
			case "id", "name", "parentId", "role", "sortOrder", "totalEmails", "unreadEmails", "totalThreads", "unreadThreads", "myRights", "isSubscribed":
				properties[p] = true
			default:
				return methodError("invalidArguments", id)
			}
		}
	}
	boxes, state, err := j.store.(mailstate.MailboxStore).MailboxSnapshot(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	if !explicit && len(boxes) > maxJMAPObjects {
		return methodError("tooManyObjectsInGet", id)
	}
	paths := map[string]string{}
	for _, m := range boxes {
		paths[m.Path] = m.ID
	}
	list := []map[string]interface{}{}
	missing := []string{}
	for _, m := range boxes {
		if explicit && !wanted[m.ID] {
			continue
		}
		delete(wanted, m.ID)
		name := m.Path
		var parent, role interface{}
		if i := strings.LastIndex(name, "/"); i >= 0 {
			parent = paths[name[:i]]
			name = name[i+1:]
		}
		if m.Role != "" {
			role = m.Role
		}
		obj := map[string]interface{}{"id": m.ID, "name": name, "parentId": parent, "role": role, "sortOrder": m.SortOrder, "isSubscribed": m.Subscribed, "totalEmails": m.Total, "unreadEmails": m.Unread, "totalThreads": m.Total, "unreadThreads": m.Unread, "myRights": map[string]bool{"mayReadItems": true, "mayAddItems": j.emailMutations(), "mayRemoveItems": j.emailMutations(), "maySetSeen": j.keywordWrites(), "maySetKeywords": j.keywordWrites(), "mayCreateChild": true, "mayRename": m.Path != "INBOX", "mayDelete": m.Path != "INBOX", "maySubmit": false}}
		if project {
			for key := range obj {
				if !properties[key] {
					delete(obj, key)
				}
			}
		}
		list = append(list, obj)
	}
	for key := range wanted {
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return MethodResponse{Name: "Mailbox/get", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "state": state, "list": list, "notFound": missing}}
}
func mailboxPatch(value interface{}, create bool) (mailstate.MailboxPatch, bool) {
	p := mailstate.MailboxPatch{}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return p, false
	}
	for key, v := range obj {
		switch key {
		case "name":
			s, ok := v.(string)
			if !ok {
				return p, false
			}
			p.Name = &s
		case "parentId":
			s := ""
			if v != nil {
				var ok bool
				s, ok = v.(string)
				if !ok || s == "" {
					return p, false
				}
			}
			p.Parent = &s
		case "isSubscribed":
			b, ok := v.(bool)
			if !ok {
				return p, false
			}
			p.Subscribed = &b
		case "sortOrder":
			n, ok := v.(float64)
			if !ok || n < 0 || n > 9007199254740991 || n != float64(int(n)) {
				return p, false
			}
			i := int(n)
			p.SortOrder = &i
		case "role":
			if !create || v != nil {
				return p, false
			}
		default:
			return p, false
		}
	}
	return p, !create || p.Name != nil
}
func (j *JMAPServer) mailboxSet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	store, ok := j.store.(mailstate.MailboxStore)
	if !ok {
		return methodError("accountReadOnly", id)
	}
	for key := range args {
		switch key {
		case "accountId", "ifInState", "create", "update", "destroy", "onDestroyRemoveEmails":
		default:
			return methodError("invalidArguments", id)
		}
	}
	if v, exists := args["onDestroyRemoveEmails"]; exists && v != false {
		return methodError("invalidArguments", id)
	}
	since := ""
	if v := args["ifInState"]; v != nil {
		var ok bool
		since, ok = v.(string)
		if !ok {
			return methodError("invalidArguments", id)
		}
		if since == "" {
			return methodError("stateMismatch", id)
		}
	}
	create, update := map[string]mailstate.MailboxPatch{}, map[string]mailstate.MailboxPatch{}
	nc, nu := map[string]interface{}{}, map[string]interface{}{}
	total := 0
	for key, target := range map[string]map[string]mailstate.MailboxPatch{"create": create, "update": update} {
		if v := args[key]; v != nil {
			obj, ok := v.(map[string]interface{})
			if !ok {
				return methodError("invalidArguments", id)
			}
			total += len(obj)
			for k, v := range obj {
				p, ok := mailboxPatch(v, key == "create")
				if ok {
					target[k] = p
				} else {
					e := map[string]interface{}{"type": "invalidProperties", "description": "Supported properties: name, parentId, isSubscribed, sortOrder"}
					if key == "create" {
						nc[k] = e
					} else {
						nu[k] = e
					}
				}
			}
		}
	}
	destroy := []string{}
	if v := args["destroy"]; v != nil {
		destroy = toStringSlice(v)
		if destroy == nil {
			return methodError("invalidArguments", id)
		}
	}
	if total+len(destroy) > maxJMAPObjects {
		return methodError("tooManyObjectsInSet", id)
	}
	result, err := store.SetMailboxes(ctx, user, since, create, update, destroy)
	if errors.Is(err, mailstate.ErrStateMismatch) {
		return methodError("stateMismatch", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	created, updated, nd := map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}
	for key, v := range result.Created {
		created[key] = map[string]interface{}{"id": v}
	}
	for _, key := range result.Updated {
		updated[key] = nil
	}
	for key, v := range result.NotCreated {
		nc[key] = map[string]interface{}{"type": v}
	}
	for key, v := range result.NotUpdated {
		nu[key] = map[string]interface{}{"type": v}
	}
	for key, v := range result.NotDestroyed {
		nd[key] = map[string]interface{}{"type": v}
	}
	return MethodResponse{Name: "Mailbox/set", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": result.OldState, "newState": result.NewState, "created": created, "updated": updated, "destroyed": result.Destroyed, "notCreated": nc, "notUpdated": nu, "notDestroyed": nd}}
}
func (j *JMAPServer) mailboxChanges(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	for key := range args {
		if key != "accountId" && key != "sinceState" && key != "maxChanges" {
			return methodError("invalidArguments", id)
		}
	}
	since, ok := args["sinceState"].(string)
	if !ok {
		return methodError("invalidArguments", id)
	}
	limit := maxJMAPObjects
	if v := args["maxChanges"]; v != nil {
		n, ok := v.(float64)
		if !ok || n < 1 || n > 9007199254740991 || n != float64(int(n)) {
			return methodError("invalidArguments", id)
		}
		if n < float64(limit) {
			limit = int(n)
		}
	}
	store, ok := j.store.(mailstate.MailboxStore)
	if !ok {
		return methodError("cannotCalculateChanges", id)
	}
	r, err := store.MailboxChanges(ctx, user, since, limit)
	if errors.Is(err, mailstate.ErrCannotCalculate) {
		return methodError("cannotCalculateChanges", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	return MethodResponse{Name: "Mailbox/changes", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": r.OldState, "newState": r.NewState, "hasMoreChanges": r.HasMore, "created": r.Created, "updated": r.Updated, "destroyed": r.Destroyed, "updatedProperties": nil}}
}
