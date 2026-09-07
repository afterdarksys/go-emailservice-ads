package jmap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func querySignature(args map[string]interface{}) string {
	filter := args["filter"]
	if filter == nil {
		filter = map[string]interface{}{}
	}
	b, _ := json.Marshal(map[string]interface{}{"filter": filter, "sort": args["sort"], "collapseThreads": args["collapseThreads"]})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func (j *JMAPServer) rememberQuery(ctx context.Context, user, kind, state string, args map[string]interface{}, ids []string) (string, bool, error) {
	s, ok := j.store.(mailstate.SnapshotStore)
	if !ok || len(ids) > 10000 {
		return state, false, nil
	}
	sig := querySignature(args)
	b, _ := json.Marshal([]interface{}{user, kind, sig, state, ids})
	token := fmt.Sprintf("q1:%x", sha256.Sum256(b))
	err := s.SaveSnapshot(ctx, user, kind, sig, token, ids)
	return token, err == nil, err
}
func numericLimit(args map[string]interface{}, key string, defaultValue int) (int, bool) {
	if v := args[key]; v != nil {
		n, ok := v.(float64)
		if !ok || n < 0 || n > float64(1<<31) || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	}
	return defaultValue, true
}
func (j *JMAPServer) queryChanges(ctx context.Context, user, kind string, args map[string]interface{}, id string) MethodResponse {
	for key := range args {
		switch key {
		case "accountId", "filter", "sort", "sinceQueryState", "maxChanges", "calculateTotal", "collapseThreads":
		default:
			return methodError("invalidArguments", id)
		}
	}
	since, ok := args["sinceQueryState"].(string)
	if !ok {
		return methodError("invalidArguments", id)
	}
	limit, ok := numericLimit(args, "maxChanges", maxJMAPObjects)
	if !ok || limit == 0 {
		return methodError("invalidArguments", id)
	}
	if limit > maxJMAPObjects {
		limit = maxJMAPObjects
	}
	if v := args["calculateTotal"]; v != nil {
		if _, ok := v.(bool); !ok {
			return methodError("invalidArguments", id)
		}
	}
	s, ok := j.store.(mailstate.SnapshotStore)
	if !ok {
		return methodError("cannotCalculateChanges", id)
	}
	old, err := s.LoadSnapshot(ctx, user, kind, querySignature(args), since)
	if errors.Is(err, mailstate.ErrCannotCalculate) {
		return methodError("cannotCalculateChanges", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	queryArgs := map[string]interface{}{"filter": args["filter"], "sort": args["sort"], "limit": float64(0), "collapseThreads": args["collapseThreads"]}
	var r MethodResponse
	if kind == "Email" {
		r = j.emailQuery(ctx, user, queryArgs, id)
	} else {
		if args["collapseThreads"] != nil {
			return methodError("invalidArguments", id)
		}
		delete(queryArgs, "collapseThreads")
		r = j.submissionQuery(ctx, user, queryArgs, id)
	}
	if r.Name == "error" {
		return r
	}
	if r.Arguments["canCalculateChanges"] != true {
		return methodError("cannotCalculateChanges", id)
	}
	state := r.Arguments["queryState"].(string)
	current, err := s.LoadSnapshot(ctx, user, kind, querySignature(args), state)
	if err != nil {
		return methodError("serverFail", id)
	}
	before, after := map[string]bool{}, map[string]bool{}
	for _, v := range old {
		before[v] = true
	}
	for _, v := range current {
		after[v] = true
	}
	removed := []string{}
	added := []map[string]interface{}{}
	for _, v := range old {
		if !after[v] {
			removed = append(removed, v)
		}
	}
	for i, v := range current {
		if !before[v] {
			added = append(added, map[string]interface{}{"id": v, "index": i})
		}
	}
	if len(removed)+len(added) > limit {
		return methodError("tooManyChanges", id)
	}
	out := map[string]interface{}{"accountId": "primary", "oldQueryState": since, "newQueryState": state, "removed": removed, "added": added}
	if args["calculateTotal"] == true {
		out["total"] = len(current)
	}
	return MethodResponse{Name: kind + "/queryChanges", CallID: id, Arguments: out}
}

func (j *JMAPServer) rememberSubmissions(ctx context.Context, user string, list []mailstate.Submission) (string, error) {
	state := submissionState(user, list)
	if s, ok := j.store.(mailstate.SnapshotStore); ok && len(list) <= 10000 {
		ids := []string{}
		for _, r := range list {
			ids = append(ids, receiptVersion(r))
		}
		if err := s.SaveSnapshot(ctx, user, "receipts", "", state, ids); err != nil {
			return "", err
		}
	}
	return state, nil
}
func (j *JMAPServer) submissionChanges(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	if j.submitter == nil {
		return methodError("unknownMethod", id)
	}
	for key := range args {
		if key != "accountId" && key != "sinceState" && key != "maxChanges" {
			return methodError("invalidArguments", id)
		}
	}
	since, ok := args["sinceState"].(string)
	if !ok {
		return methodError("invalidArguments", id)
	}
	limit, ok := numericLimit(args, "maxChanges", maxJMAPObjects)
	if !ok || limit == 0 {
		return methodError("invalidArguments", id)
	}
	if limit > maxJMAPObjects {
		limit = maxJMAPObjects
	}
	s, ok := j.store.(mailstate.SnapshotStore)
	if !ok {
		return methodError("cannotCalculateChanges", id)
	}
	j.submissionMu.Lock()
	defer j.submissionMu.Unlock()
	old, err := s.LoadSnapshot(ctx, user, "receipts", "", since)
	if errors.Is(err, mailstate.ErrCannotCalculate) {
		return methodError("cannotCalculateChanges", id)
	}
	if err != nil {
		return methodError("serverFail", id)
	}
	list, err := j.submitter.Submissions(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	state, err := j.rememberSubmissions(ctx, user, list)
	if err != nil {
		return methodError("serverFail", id)
	}
	if len(list) > 10000 {
		return methodError("cannotCalculateChanges", id)
	}
	before, after := map[string]string{}, map[string]string{}
	for _, v := range old {
		id, _, _ := strings.Cut(v, "\x00")
		before[id] = v
	}
	for _, v := range list {
		after[v.ID] = receiptVersion(v)
	}
	created, removed, updated := []string{}, []string{}, []string{}
	for _, v := range list {
		if previous, exists := before[v.ID]; !exists {
			created = append(created, v.ID)
		} else if previous != after[v.ID] {
			updated = append(updated, v.ID)
		}
	}
	for id := range before {
		if _, exists := after[id]; !exists {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	more := len(created)+len(removed)+len(updated) > limit
	if more {
		removed = removed[:min(len(removed), limit)]
		left := limit - len(removed)
		created = created[:min(len(created), left)]
		left -= len(created)
		updated = updated[:min(len(updated), left)]
		for _, id := range removed {
			delete(before, id)
		}
		for _, id := range append(append([]string{}, created...), updated...) {
			before[id] = after[id]
		}
		versions := []string{}
		for _, v := range before {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		b, _ := json.Marshal([]interface{}{user, versions})
		state = fmt.Sprintf("sp2:%x", sha256.Sum256(b))
		if err = s.SaveSnapshot(ctx, user, "receipts", "", state, versions); err != nil {
			return methodError("serverFail", id)
		}
	}
	return MethodResponse{Name: "EmailSubmission/changes", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": since, "newState": state, "hasMoreChanges": more, "created": created, "updated": updated, "destroyed": removed}}

}

func (j *JMAPServer) submissionQuery(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	if j.submitter == nil {
		return methodError("unknownMethod", id)
	}
	for key := range args {
		switch key {
		case "accountId", "filter", "sort", "position", "anchor", "anchorOffset", "limit", "calculateTotal":
		default:
			return methodError("invalidArguments", id)
		}
	}
	type comparator struct {
		property  string
		ascending bool
	}
	comparators := []comparator{}
	if v := args["sort"]; v != nil {
		list, ok := v.([]interface{})
		if !ok || len(list) > 3 {
			return methodError("unsupportedSort", id)
		}
		for _, v := range list {
			obj, ok := v.(map[string]interface{})
			if !ok {
				return methodError("unsupportedSort", id)
			}
			for key := range obj {
				if key != "property" && key != "isAscending" {
					return methodError("unsupportedSort", id)
				}
			}
			property, ok := obj["property"].(string)
			if !ok {
				return methodError("unsupportedSort", id)
			}
			switch property {
			case "emailId", "threadId", "sendAt", "sentAt":
			default:
				return methodError("unsupportedSort", id)
			}
			ascending := true
			if v := obj["isAscending"]; v != nil {
				var ok bool
				ascending, ok = v.(bool)
				if !ok {
					return methodError("unsupportedSort", id)
				}
			}
			comparators = append(comparators, comparator{property, ascending})
		}
	}
	if len(comparators) == 0 {
		comparators = []comparator{{"sendAt", false}}
	}
	if v := args["calculateTotal"]; v != nil {
		if _, ok := v.(bool); !ok {
			return methodError("invalidArguments", id)
		}
	}
	filter := map[string]interface{}{}
	if args["filter"] != nil {
		var ok bool
		filter, ok = args["filter"].(map[string]interface{})
		if !ok {
			return methodError("invalidArguments", id)
		}
	}
	for key, v := range filter {
		switch key {
		case "identityIds", "emailIds", "threadIds":
			if toStringSlice(v) == nil {
				return methodError("invalidArguments", id)
			}
		case "undoStatus":
			if _, ok := v.(string); !ok {
				return methodError("invalidArguments", id)
			}
		case "before", "after":
			str, ok := v.(string)
			if !ok || !strings.HasSuffix(str, "Z") {
				return methodError("invalidArguments", id)
			}
			if _, err := time.Parse(time.RFC3339, str); err != nil {
				return methodError("invalidArguments", id)
			}
		default:
			return methodError("unsupportedFilter", id)
		}
	}
	j.submissionMu.Lock()
	defer j.submissionMu.Unlock()
	list, err := j.submitter.Submissions(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	state, err := j.rememberSubmissions(ctx, user, list)
	if err != nil {
		return methodError("serverFail", id)
	}
	value := func(r mailstate.Submission, key string) string {
		switch key {
		case "emailId":
			return r.EmailID
		case "threadId":
			return r.ThreadID
		default:
			return r.SendAt
		}
	}
	sort.Slice(list, func(a, b int) bool {
		for _, c := range comparators {
			x, y := value(list[a], c.property), value(list[b], c.property)
			if x != y {
				if c.ascending {
					return x < y
				}
				return x > y
			}
		}
		return list[a].ID < list[b].ID
	})
	ids := []string{}
	for _, r := range list {
		match := true
		for key, v := range filter {
			switch key {
			case "identityIds", "emailIds", "threadIds":
				want := map[string]string{"identityIds": r.IdentityID, "emailIds": r.EmailID, "threadIds": r.ThreadID}[key]
				found := false
				for _, x := range toStringSlice(v) {
					found = found || x == want
				}
				match = match && found
			case "undoStatus":
				match = match && v == r.UndoStatus
			case "before", "after":
				at, _ := time.Parse(time.RFC3339, r.SendAt)
				boundary, _ := time.Parse(time.RFC3339, v.(string))
				match = match && (key == "before" && at.Before(boundary) || key == "after" && !at.Before(boundary))
			}
		}
		if match {
			ids = append(ids, r.ID)
		}
	}
	state, can, err := j.rememberQuery(ctx, user, "EmailSubmission", state, args, ids)
	if err != nil {
		return methodError("serverFail", id)
	}
	total := len(ids)
	position, start, end, kind := queryPage(args, ids)
	if kind != "" {
		return methodError(kind, id)
	}
	return MethodResponse{Name: "EmailSubmission/query", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "queryState": state, "canCalculateChanges": can, "position": position, "ids": ids[start:end], "total": total}}
}

func receiptVersion(s mailstate.Submission) string {
	b, _ := json.Marshal(s)
	return fmt.Sprintf("%s\x00%x", s.ID, sha256.Sum256(b))
}
