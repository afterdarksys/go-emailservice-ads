package jmap

import (
	"context"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func (s *recordingSubmitter) DestroySubmission(ctx context.Context, user, id string) error {
	for i, r := range s.records[user] {
		if r.ID == id {
			s.records[user] = append(s.records[user][:i], s.records[user][i+1:]...)
			return nil
		}
	}
	return mailstate.ErrBlobNotFound
}
func TestSubmissionSuccessHooksAndReceiptChanges(t *testing.T) {
	j, store, folder := compositionServer(t)
	sub := &recordingSubmitter{records: map[string][]mailstate.Submission{}}
	j.SetSubmitter(sub)
	ctx := context.WithValue(context.Background(), authUserKey, "alice")
	boxes, _, err := store.MailboxSnapshot(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sent := ""
	for _, b := range boxes {
		if b.Path == "Sent" {
			sent = b.ID
		}
	}
	call := func(name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, ID: "a", Arguments: args})
	}
	initial := call("EmailSubmission/get", nil).Arguments["state"]
	response := j.processRequest(ctx, &Request{MethodCalls: []MethodCall{
		{Name: "Email/set", ID: "draft", Arguments: map[string]interface{}{"create": map[string]interface{}{"draft": draftObject(folder)}}},
		{Name: "EmailSubmission/set", ID: "send", Arguments: map[string]interface{}{"create": map[string]interface{}{"send": map[string]interface{}{"emailId": "#draft", "identityId": "primary"}}, "onSuccessUpdateEmail": map[string]interface{}{"#send": map[string]interface{}{"mailboxIds": map[string]interface{}{sent: true}, "keywords/$draft": nil}}}},
	}})
	if len(response.MethodResponses) != 3 || response.MethodResponses[2].Name != "Email/set" || response.MethodResponses[2].CallID != "send" {
		t.Fatal(response)
	}
	mid := response.CreatedIds["draft"]
	emails, _, _ := store.EmailSnapshot(ctx, "alice")
	if emails[mid].Folder != "Sent" || keywords(emails[mid].Flags)["$draft"] {
		t.Fatal(emails)
	}
	changes := call("EmailSubmission/changes", map[string]interface{}{"sinceState": initial})
	if len(changes.Arguments["created"].([]string)) != 1 {
		t.Fatal(changes)
	}
	query := call("EmailSubmission/query", map[string]interface{}{"filter": map[string]interface{}{"emailIds": []interface{}{mid}}})
	if query.Arguments["total"] != 1 || query.Arguments["canCalculateChanges"] != true {
		t.Fatal(query)
	}
	deleted := call("EmailSubmission/set", map[string]interface{}{"destroy": []interface{}{"receipt"}})
	if len(deleted.Arguments["destroyed"].([]string)) != 1 {
		t.Fatal(deleted)
	}
	changes = call("EmailSubmission/changes", map[string]interface{}{"sinceState": changes.Arguments["newState"]})
	if len(changes.Arguments["destroyed"].([]string)) != 1 {
		t.Fatal(changes)
	}
	delta := call("EmailSubmission/queryChanges", map[string]interface{}{"filter": map[string]interface{}{"emailIds": []interface{}{mid}}, "sinceQueryState": query.Arguments["queryState"]})
	if len(delta.Arguments["removed"].([]string)) != 1 {
		t.Fatal(delta)
	}
	if _, err = store.FetchMessage(ctx, mid); err != nil {
		t.Fatal("receipt deletion deleted email", err)
	}
	// A successful send stays successful even if the subsequent email patch fails.
	r := call("EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"send": map[string]interface{}{"emailId": mid, "identityId": "primary"}}, "onSuccessUpdateEmail": map[string]interface{}{"#send": map[string]interface{}{"subject": "immutable"}}})
	if len(r.Arguments["created"].(map[string]interface{})) != 1 || len(r.Followups) != 1 || len(r.Followups[0].Arguments["notUpdated"].(map[string]interface{})) != 1 {
		t.Fatal(r)
	}
	// Only successful submissions may trigger deletion.
	r = call("EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"bad": map[string]interface{}{"emailId": "missing", "identityId": "primary"}}, "onSuccessDestroyEmail": []interface{}{"#bad"}})
	if len(r.Followups) != 1 || len(r.Followups[0].Arguments["destroyed"].([]string)) != 0 {
		t.Fatal(r)
	}
	r = call("EmailSubmission/set", map[string]interface{}{"destroy": []interface{}{"receipt-2"}, "onSuccessDestroyEmail": []interface{}{"receipt-2"}})
	if len(r.Followups[0].Arguments["destroyed"].([]string)) != 1 {
		t.Fatal(r)
	}
}

func TestEmailQueryChangesMembershipIndicesAndScope(t *testing.T) {
	j, s, _ := compositionServer(t)
	ctx := context.Background()
	call := func(user, name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, user, MethodCall{Name: name, ID: "q", Arguments: args})
	}
	initial := call("alice", "Email/query", map[string]interface{}{"filter": map[string]interface{}{"hasKeyword": "$seen"}})
	if initial.Arguments["canCalculateChanges"] != true {
		t.Fatal(initial)
	}
	a, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: a\r\n\r\na"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: b\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetEmailKeywords(ctx, "alice", "", map[string]mailstate.Patch{a: {Add: []string{`\Seen`}}, b: {Add: []string{`\Seen`}}}); err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{"filter": map[string]interface{}{"hasKeyword": "$seen"}, "sinceQueryState": initial.Arguments["queryState"], "maxChanges": float64(1)}
	if r := call("alice", "Email/queryChanges", args); r.Arguments["type"] != "tooManyChanges" {
		t.Fatal(r)
	}
	args["maxChanges"] = float64(5)
	args["calculateTotal"] = true
	r := call("alice", "Email/queryChanges", args)
	if len(r.Arguments["added"].([]map[string]interface{})) != 2 || r.Arguments["total"] != 2 {
		t.Fatal(r)
	}
	for i, item := range r.Arguments["added"].([]map[string]interface{}) {
		if item["index"] != i {
			t.Fatal(r)
		}
	}
	if _, err = s.SetEmailKeywords(ctx, "alice", "", map[string]mailstate.Patch{a: {Remove: []string{`\Seen`}}}); err != nil {
		t.Fatal(err)
	}
	args["sinceQueryState"] = r.Arguments["newQueryState"]
	r = call("alice", "Email/queryChanges", args)
	if len(r.Arguments["removed"].([]string)) != 1 || r.Arguments["removed"].([]string)[0] != a || len(r.Arguments["added"].([]map[string]interface{})) != 0 {
		t.Fatal(r)
	}
	if r = call("bob", "Email/queryChanges", args); r.Arguments["type"] != "cannotCalculateChanges" {
		t.Fatal(r)
	}
	args["filter"] = map[string]interface{}{"hasKeyword": "$flagged"}
	if r = call("alice", "Email/queryChanges", args); r.Arguments["type"] != "cannotCalculateChanges" {
		t.Fatal(r)
	}
}

func TestSubmissionPagingAndMergedSuccessHooks(t *testing.T) {
	j, s, _ := compositionServer(t)
	sub := &recordingSubmitter{records: map[string][]mailstate.Submission{}}
	j.SetSubmitter(sub)
	ctx := context.Background()
	call := func(name string, args map[string]interface{}) MethodResponse {
		return j.processMethodCall(ctx, "alice", MethodCall{Name: name, ID: "a", Arguments: args})
	}
	initial := call("EmailSubmission/get", nil).Arguments["state"]
	mid, err := s.StoreMessage(ctx, "alice", "INBOX", []byte("From: alice@example.test\r\nTo: bob@example.test\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	item := map[string]interface{}{"emailId": mid, "identityId": "primary"}
	r := call("EmailSubmission/set", map[string]interface{}{"create": map[string]interface{}{"a": item, "b": item}, "onSuccessUpdateEmail": map[string]interface{}{"#a": map[string]interface{}{"keywords/$seen": true}, "#b": map[string]interface{}{"keywords/$flagged": true}}})
	if len(r.Arguments["created"].(map[string]interface{})) != 2 {
		t.Fatal(r)
	}
	emails, _, _ := s.EmailSnapshot(ctx, "alice")
	if !keywords(emails[mid].Flags)["$seen"] || !keywords(emails[mid].Flags)["$flagged"] {
		t.Fatal(emails)
	}
	page := call("EmailSubmission/changes", map[string]interface{}{"sinceState": initial, "maxChanges": float64(1)})
	if page.Arguments["hasMoreChanges"] != true || len(page.Arguments["created"].([]string)) != 1 {
		t.Fatal(page)
	}
	last := call("EmailSubmission/changes", map[string]interface{}{"sinceState": page.Arguments["newState"], "maxChanges": float64(1)})
	if last.Arguments["hasMoreChanges"] != false || len(last.Arguments["created"].([]string)) != 1 || last.Arguments["created"].([]string)[0] == page.Arguments["created"].([]string)[0] {
		t.Fatal(last)
	}
	query := call("EmailSubmission/query", map[string]interface{}{"sort": []interface{}{map[string]interface{}{"property": "emailId", "isAscending": true}}, "limit": float64(1), "position": float64(1)})
	if query.Arguments["total"] != 2 || len(query.Arguments["ids"].([]string)) != 1 {
		t.Fatal(query)
	}
}
