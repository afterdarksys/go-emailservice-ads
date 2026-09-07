package jmap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func (j *JMAPServer) threadSnapshot(ctx context.Context, user string) (map[string][]string, string, error) {
	owned, _, err := j.emailSnapshot(ctx, user)
	if err != nil {
		return nil, "", err
	}
	threads := map[string][]string{}
	for id, meta := range owned {
		tid := meta.ThreadID
		if tid == "" {
			raw, err := j.store.FetchMessage(ctx, id)
			if err != nil {
				return nil, "", err
			}
			tid = mailstate.ThreadID(id, raw)
		}
		threads[tid] = append(threads[tid], id)
	}
	for _, ids := range threads {
		sort.Slice(ids, func(a, b int) bool {
			if owned[ids[a]].Date.Equal(owned[ids[b]].Date) {
				return ids[a] < ids[b]
			}
			return owned[ids[a]].Date.Before(owned[ids[b]].Date)
		})
	}
	raw, _ := json.Marshal(threads)
	state := fmt.Sprintf("t1:%x", sha256.Sum256(append([]byte(user+"\x00"), raw...)))
	if s, ok := j.store.(mailstate.SnapshotStore); ok && len(threads) <= 10000 {
		versions := []string{}
		for tid, ids := range threads {
			b, _ := json.Marshal(ids)
			versions = append(versions, tid+"\x00"+string(b))
		}
		sort.Strings(versions)
		if err := s.SaveSnapshot(ctx, user, "Thread", "", state, versions); err != nil {
			return nil, "", err
		}
	}
	return threads, state, nil
}

func (j *JMAPServer) threadGet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	for k := range args {
		if k != "accountId" && k != "ids" && k != "properties" {
			return methodError("invalidArguments", id)
		}
	}
	if args["ids"] == nil {
		return methodError("invalidArguments", id)
	}
	ids := toStringSlice(args["ids"])
	if ids == nil {
		return methodError("invalidArguments", id)
	}
	if len(ids) > maxJMAPObjects {
		return methodError("tooManyObjectsInGet", id)
	}
	if args["properties"] != nil {
		props := toStringSlice(args["properties"])
		if props == nil {
			return methodError("invalidArguments", id)
		}
		for _, p := range props {
			if p != "id" && p != "emailIds" {
				return methodError("invalidArguments", id)
			}
		}
	}
	threads, state, err := j.threadSnapshot(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	list := []map[string]interface{}{}
	missing := []string{}
	for _, tid := range ids {
		if emails, ok := threads[tid]; ok {
			o := map[string]interface{}{"id": tid}
			if args["properties"] == nil || strings.Contains(" "+strings.Join(toStringSlice(args["properties"]), " ")+" ", " emailIds ") {
				o["emailIds"] = emails
			}
			list = append(list, o)
		} else {
			missing = append(missing, tid)
		}
	}
	return MethodResponse{Name: "Thread/get", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "state": state, "list": list, "notFound": missing}}
}

func (j *JMAPServer) threadChanges(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	for k := range args {
		if k != "accountId" && k != "sinceState" && k != "maxChanges" {
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
	s, ok := j.store.(mailstate.SnapshotStore)
	if !ok {
		return methodError("cannotCalculateChanges", id)
	}
	old, err := s.LoadSnapshot(ctx, user, "Thread", "", since)
	if err != nil {
		return methodError("cannotCalculateChanges", id)
	}
	_, state, err := j.threadSnapshot(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	current, err := s.LoadSnapshot(ctx, user, "Thread", "", state)
	if err != nil {
		return methodError("cannotCalculateChanges", id)
	}
	before := map[string]string{}
	after := map[string]string{}
	for _, v := range old {
		k, b, _ := strings.Cut(v, "\x00")
		before[k] = b
	}
	for _, v := range current {
		k, b, _ := strings.Cut(v, "\x00")
		after[k] = b
	}
	created, updated, destroyed := []string{}, []string{}, []string{}
	for k, v := range after {
		if b, ok := before[k]; !ok {
			created = append(created, k)
		} else if b != v {
			updated = append(updated, k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			destroyed = append(destroyed, k)
		}
	}
	if len(created)+len(updated)+len(destroyed) > limit {
		return methodError("cannotCalculateChanges", id)
	}
	sort.Strings(created)
	sort.Strings(updated)
	sort.Strings(destroyed)
	return MethodResponse{Name: "Thread/changes", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": since, "newState": state, "hasMoreChanges": false, "created": created, "updated": updated, "destroyed": destroyed}}
}
