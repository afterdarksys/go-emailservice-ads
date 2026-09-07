package jmap

import (
	"context"
	"testing"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

type scheduledRecorder struct {
	recordingSubmitter
	at time.Time
}

func (s *scheduledRecorder) SubmitAt(ctx context.Context, user, ip, mid, from string, to []string, data []byte, at time.Time) (mailstate.Submission, error) {
	s.at = at
	r, err := s.Submit(ctx, user, ip, mid, from, to, data)
	if err != nil {
		return r, err
	}
	r.UndoStatus = "pending"
	r.SendAt = at.UTC().Format(time.RFC3339)
	s.records[user][len(s.records[user])-1] = r
	return r, nil
}
func (s *scheduledRecorder) CancelSubmission(ctx context.Context, user, id string) error {
	for i, r := range s.records[user] {
		if r.ID == id {
			s.records[user][i].UndoStatus = "canceled"
			return nil
		}
	}
	return mailstate.ErrCannotUnsend
}
func TestScheduledSubmissionWireAndUpdatedChanges(t *testing.T) {
	j, _, folder := compositionServer(t)
	ctx := context.Background()
	sub := &scheduledRecorder{recordingSubmitter: recordingSubmitter{records: map[string][]mailstate.Submission{}}}
	j.SetSubmitter(sub)
	call := func(name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, Arguments: args, ID: "s"})
	}
	draft := call("Email/set", map[string]interface{}{"create": map[string]interface{}{"d": draftObject(folder)}}).Arguments["created"].(map[string]interface{})["d"].(map[string]interface{})["id"]
	object := map[string]interface{}{"emailId": draft, "identityId": "primary", "envelope": map[string]interface{}{"mailFrom": map[string]interface{}{"email": "alice@example.test", "parameters": map[string]interface{}{"HOLDFOR": "3600"}}, "rcptTo": []interface{}{map[string]interface{}{"email": "bob@example.test"}}}}
	created := call("EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"s": object}})
	if len(created.Arguments["created"].(map[string]interface{})) != 1 || time.Until(sub.at) < 59*time.Minute {
		t.Fatal(created, sub.at)
	}
	state := created.Arguments["newState"]
	canceled := call("EmailSubmission/set", map[string]interface{}{"ifInState": state, "update": map[string]interface{}{"receipt": map[string]interface{}{"undoStatus": "canceled"}}})
	if len(canceled.Arguments["updated"].(map[string]interface{})) != 1 || canceled.Arguments["newState"] == state {
		t.Fatal(canceled)
	}
	delta := call("EmailSubmission/changes", map[string]interface{}{"sinceState": state})
	if ids := delta.Arguments["updated"].([]string); len(ids) != 1 || ids[0] != "receipt" {
		t.Fatal(delta)
	}
	got := call("EmailSubmission/get", nil)
	if got.Arguments["state"] != canceled.Arguments["newState"] {
		t.Fatal(got, canceled)
	}
	object["envelope"].(map[string]interface{})["mailFrom"].(map[string]interface{})["parameters"] = map[string]interface{}{"HOLDFOR": "999999999"}
	bad := call("EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"bad": object}})
	if len(bad.Arguments["notCreated"].(map[string]interface{})) != 1 || sub.calls != 1 {
		t.Fatal(bad, sub.calls)
	}
}
