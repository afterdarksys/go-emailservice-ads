package jmap

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func (j *JMAPServer) keywordWrites() bool { _, ok := j.store.(mailstate.Store); return ok }
func (j *JMAPServer) emailSnapshot(ctx context.Context, user string) (map[string]MessageOwnedSummary, string, error) {
	if store, ok := j.store.(mailstate.Store); ok {
		return store.EmailSnapshot(ctx, user)
	}
	messages, err := j.ownedMessages(ctx, user)
	return messages, emailState(messages), err
}
func (j *JMAPServer) emailChanges(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
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
	if value := args["maxChanges"]; value != nil {
		n, ok := value.(float64)
		if !ok || n < 1 || n != float64(int(n)) {
			return methodError("invalidArguments", id)
		}
		if n < float64(limit) {
			limit = int(n)
		}
	}
	store, ok := j.store.(mailstate.Store)
	if !ok {
		return methodError("cannotCalculateChanges", id)
	}
	result, err := store.EmailChanges(ctx, user, since, limit)
	if errors.Is(err, mailstate.ErrCannotCalculate) {
		return methodError("cannotCalculateChanges", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	return MethodResponse{Name: "Email/changes", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": result.OldState, "newState": result.NewState, "hasMoreChanges": result.HasMore, "created": result.Created, "updated": result.Updated, "destroyed": result.Destroyed}}
}

func keywordFlag(key string) (string, bool) {
	if len(key) < 1 || len(key) > 255 {
		return "", false
	}
	for _, r := range key {
		if r <= 32 || r >= 127 || strings.ContainsRune("(){%*\\\"]}", r) {
			return "", false
		}
	}
	switch strings.ToLower(key) {
	case "$seen":
		return `\Seen`, true
	case "$flagged":
		return `\Flagged`, true
	case "$answered":
		return `\Answered`, true
	case "$draft":
		return `\Draft`, true
	}
	return key, true
}
func keywordPatch(value interface{}) (mailstate.Patch, bool) {
	patch := mailstate.Patch{}
	object, ok := value.(map[string]interface{})
	if !ok {
		return patch, false
	}
	if replacement, exists := object["keywords"]; exists {
		if len(object) != 1 {
			return patch, false
		}
		keywords, ok := replacement.(map[string]interface{})
		if !ok {
			return patch, false
		}
		flags := []string{}
		for key, v := range keywords {
			flag, valid := keywordFlag(key)
			if !valid || v != true {
				return patch, false
			}
			flags = append(flags, flag)
		}
		sort.Strings(flags)
		patch.Replace = &flags
		return patch, true
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, path := range keys {
		v := object[path]
		if !strings.HasPrefix(path, "keywords/") {
			return patch, false
		}
		key := strings.TrimPrefix(path, "keywords/")
		// Decode one JSON Pointer token; reject malformed escapes or nested paths.
		if strings.Contains(key, "/") {
			return patch, false
		}
		var decoded strings.Builder
		for n := 0; n < len(key); n++ {
			if key[n] == '~' {
				n++
				if n == len(key) || (key[n] != '0' && key[n] != '1') {
					return patch, false
				}
				if key[n] == '0' {
					decoded.WriteByte('~')
				} else {
					decoded.WriteByte('/')
				}
			} else {
				decoded.WriteByte(key[n])
			}
		}
		flag, valid := keywordFlag(decoded.String())
		canonical := strings.ToLower(flag)
		if !valid || seen[canonical] {
			return patch, false
		}
		seen[canonical] = true
		if v == nil {
			patch.Remove = append(patch.Remove, flag)
		} else if v == true {
			patch.Add = append(patch.Add, flag)
		} else {
			return patch, false
		}
	}
	return patch, true
}

func (j *JMAPServer) emailSet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	for key := range args {
		switch key {
		case "accountId", "ifInState", "create", "update", "destroy":
		default:
			return methodError("invalidArguments", id)
		}
	}
	store, ok := j.store.(mailstate.Store)
	if !ok {
		return methodError("accountReadOnly", id)
	}
	since := ""
	if value := args["ifInState"]; value != nil {
		var ok bool
		since, ok = value.(string)
		if !ok {
			return methodError("invalidArguments", id)
		}
		if since == "" {
			return methodError("stateMismatch", id)
		}
	}
	create, update := map[string]interface{}{}, map[string]interface{}{}
	destroy := []string{}
	for key, target := range map[string]*map[string]interface{}{"create": &create, "update": &update} {
		if value := args[key]; value != nil {
			m, ok := value.(map[string]interface{})
			if !ok {
				return methodError("invalidArguments", id)
			}
			*target = m
		}
	}
	if value := args["destroy"]; value != nil {
		destroy = toStringSlice(value)
		if destroy == nil {
			return methodError("invalidArguments", id)
		}
	}
	if len(create)+len(update)+len(destroy) > maxJMAPObjects {
		return methodError("tooManyObjectsInSet", id)
	}
	if j.emailMutations() {
		return j.mutateEmails(ctx, user, since, update, create, destroy, id)
	}
	patches := map[string]mailstate.Patch{}
	notUpdated, notCreated, notDestroyed := map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}
	for mid, value := range update {
		patch, ok := keywordPatch(value)
		if !ok {
			notUpdated[mid] = map[string]interface{}{"type": "invalidProperties", "description": "Only keywords and keywords/<name> updates are supported"}
		} else {
			patches[mid] = patch
		}
	}
	for key := range create {
		notCreated[key] = map[string]interface{}{"type": "forbidden", "description": "Email creation is not supported"}
	}
	for _, key := range destroy {
		notDestroyed[key] = map[string]interface{}{"type": "forbidden", "description": "Email destruction is not supported"}
	}
	result, err := store.SetEmailKeywords(ctx, user, since, patches)
	if errors.Is(err, mailstate.ErrStateMismatch) {
		return methodError("stateMismatch", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	updated := map[string]interface{}{}
	for _, key := range result.Updated {
		updated[key] = nil
	}
	for _, key := range result.NotFound {
		notUpdated[key] = map[string]interface{}{"type": "notFound"}
	}
	return MethodResponse{Name: "Email/set", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": result.OldState, "newState": result.NewState, "created": map[string]interface{}{}, "updated": updated, "destroyed": []string{}, "notCreated": notCreated, "notUpdated": notUpdated, "notDestroyed": notDestroyed}}
}
