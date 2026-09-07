package jmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
	"github.com/emersion/go-smtp"
	"go.uber.org/zap"
)

type submissionIPKey struct{}
type creationIDsKey struct{}

func submissionState(user string, list []mailstate.Submission) string {
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, _ := json.Marshal(list)
	return fmt.Sprintf("s1:%x", sha256.Sum256(append([]byte(user+":"), data...)))
}
func getObjects(name, state string, objects []map[string]interface{}, args map[string]interface{}, id string) MethodResponse {
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
	} else if len(objects) > maxJMAPObjects {
		return methodError("tooManyObjectsInGet", id)
	}
	props := map[string]bool{"id": true}
	project := args["properties"] != nil
	if project {
		p := toStringSlice(args["properties"])
		if p == nil {
			return methodError("invalidArguments", id)
		}
		for _, key := range p {
			allowed := " id emailId identityId threadId envelope sendAt undoStatus deliveryStatus dsnBlobIds mdnBlobIds "
			if name == "Identity/get" {
				allowed = " id name email replyTo bcc textSignature htmlSignature mayDelete "
			}
			if !strings.Contains(allowed, " "+key+" ") {
				return methodError("invalidArguments", id)
			}
			props[key] = true
		}
	}
	list := []map[string]interface{}{}
	for _, obj := range objects {
		key := obj["id"].(string)
		if explicit && !wanted[key] {
			continue
		}
		delete(wanted, key)
		if project {
			for key := range obj {
				if !props[key] {
					delete(obj, key)
				}
			}
		}
		list = append(list, obj)
	}
	missing := []string{}
	for key := range wanted {
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return MethodResponse{Name: name, CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "state": state, "list": list, "notFound": missing}}
}
func (j *JMAPServer) identityEmail(user string) (string, bool) {
	if j.validator == nil {
		return "", false
	}
	account, ok := j.validator.GetUserStore().GetUser(user)
	if !ok || !account.Enabled {
		return "", false
	}
	return account.Email, account.Email != ""
}
func (j *JMAPServer) identityGet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	if j.submitter == nil {
		return methodError("unknownMethod", id)
	}
	email, ok := j.identityEmail(user)
	if !ok {
		return methodError("accountNotFound", id)
	}
	obj := map[string]interface{}{"id": "primary", "name": user, "email": email, "replyTo": nil, "bcc": nil, "textSignature": "", "htmlSignature": "", "mayDelete": false}
	return getObjects("Identity/get", fmt.Sprintf("i1:%x", sha256.Sum256([]byte(user+":"+email))), []map[string]interface{}{obj}, args, id)
}
func (j *JMAPServer) submissionGet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	if j.submitter == nil {
		return methodError("unknownMethod", id)
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
	objects := []map[string]interface{}{}
	for _, s := range list {
		data, _ := json.Marshal(s)
		obj := map[string]interface{}{}
		json.Unmarshal(data, &obj)
		obj["dsnBlobIds"] = []string{}
		obj["mdnBlobIds"] = []string{}
		objects = append(objects, obj)
	}
	return getObjects("EmailSubmission/get", state, objects, args, id)
}
func envelopeAddress(v interface{}) (string, bool) {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return "", false
	}
	for key, value := range obj {
		if key != "email" && (key != "parameters" || value != nil) {
			return "", false
		}
	}
	s, ok := obj["email"].(string)
	if !ok || !safeHeader(s) {
		return "", false
	}
	p, err := mail.ParseAddress(s)
	return s, err == nil && p.Address == s
}
func (j *JMAPServer) createSubmission(ctx context.Context, user string, v interface{}) (mailstate.Submission, map[string]interface{}) {
	empty := mailstate.Submission{}
	fail := func(kind string) (mailstate.Submission, map[string]interface{}) {
		return empty, map[string]interface{}{"type": kind}
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return fail("invalidProperties")
	}
	for key := range obj {
		if key != "emailId" && key != "identityId" && key != "envelope" {
			return fail("invalidProperties")
		}
	}
	mid, ok := obj["emailId"].(string)
	if !ok || mid == "" || obj["identityId"] != "primary" {
		return fail("invalidProperties")
	}
	if strings.HasPrefix(mid, "#") {
		ids, _ := ctx.Value(creationIDsKey{}).(map[string]string)
		mid = ids[strings.TrimPrefix(mid, "#")]
		if mid == "" {
			return fail("invalidProperties")
		}
	}
	store, ok := j.store.(mailstate.ImportStore)
	if !ok {
		return fail("forbiddenToSend")
	}
	blob, err := store.GetBlob(ctx, user, mid)
	if errors.Is(err, mailstate.ErrBlobNotFound) {
		return fail("invalidProperties")
	}
	if err != nil {
		return fail("serverFail")
	}
	// Submission must reference an email, not an unimported upload.
	emails, _, err := j.emailSnapshot(ctx, user)
	if err != nil {
		return fail("serverFail")
	}
	if _, ok := emails[mid]; !ok {
		return fail("invalidProperties")
	}
	maxSize := mailstate.MaxUploadBytes
	maxRecipients := 100
	if j.config != nil {
		if j.config.Server.MaxMessageBytes > 0 {
			maxSize = j.config.Server.MaxMessageBytes
		}
		if j.config.Server.MaxRecipients > 0 {
			maxRecipients = j.config.Server.MaxRecipients
		}
	}
	if len(blob.Data) > maxSize {
		return empty, map[string]interface{}{"type": "tooLarge", "maxSize": maxSize}
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(blob.Data))
	if err != nil {
		return fail("invalidEmail")
	}
	identity, ok := j.identityEmail(user)
	if !ok {
		return fail("forbiddenToSend")
	}
	froms, err := parsed.Header.AddressList("From")
	if err != nil || len(froms) != 1 || !strings.EqualFold(froms[0].Address, identity) {
		return fail("forbiddenFrom")
	}
	var scheduled time.Time
	from := identity
	to := []string{}
	if v := obj["envelope"]; v != nil {
		envelope, ok := v.(map[string]interface{})
		if !ok {
			return fail("invalidProperties")
		}
		for key := range envelope {
			if key != "mailFrom" && key != "rcptTo" {
				return fail("invalidProperties")
			}
		}
		sender, valid := envelope["mailFrom"].(map[string]interface{})
		if !valid {
			return fail("invalidProperties")
		}
		clean := map[string]interface{}{}
		for k, v := range sender {
			clean[k] = v
		}
		if raw := sender["parameters"]; raw != nil {
			params, ok := raw.(map[string]interface{})
			if !ok || len(params) != 1 {
				return fail("invalidProperties")
			}
			for key, value := range params {
				text, ok := value.(string)
				if !ok {
					return fail("invalidProperties")
				}
				switch strings.ToUpper(key) {
				case "HOLDFOR":
					seconds, err := strconv.ParseUint(text, 10, 32)
					if err != nil || seconds > uint64(mailstate.MaxDelayedSend/time.Second) {
						return fail("invalidProperties")
					}
					scheduled = time.Now().Add(time.Duration(seconds) * time.Second)
				case "HOLDUNTIL":
					var err error
					scheduled, err = time.Parse("20060102T150405Z", text)
					if err != nil || scheduled.After(time.Now().Add(mailstate.MaxDelayedSend)) {
						return fail("invalidProperties")
					}
				default:
					return fail("invalidProperties")
				}
			}
			delete(clean, "parameters")
		}
		from, ok = envelopeAddress(clean)
		if !ok {
			return fail("invalidProperties")
		}
		if !strings.EqualFold(from, identity) {
			return fail("forbiddenMailFrom")
		}
		recipients, ok := envelope["rcptTo"].([]interface{})
		if !ok {
			return fail("invalidProperties")
		}
		for _, v := range recipients {
			address, ok := envelopeAddress(v)
			if !ok {
				return fail("invalidProperties")
			}
			to = append(to, address)
		}
	} else {
		for _, key := range []string{"To", "Cc", "Bcc"} {
			if parsed.Header.Get(key) == "" {
				continue
			}
			recipients, e := parsed.Header.AddressList(key)
			if e != nil {
				return fail("invalidEmail")
			}
			for _, address := range recipients {
				to = append(to, address.Address)
			}
		}
	}
	seen := map[string]bool{}
	unique := []string{}
	for _, address := range to {
		if !seen[strings.ToLower(address)] {
			seen[strings.ToLower(address)] = true
			unique = append(unique, address)
		}
	}
	to = unique
	if len(to) == 0 {
		return fail("noRecipients")
	}
	if len(to) > maxRecipients {
		return empty, map[string]interface{}{"type": "tooManyRecipients", "maxRecipients": maxRecipients}
	}
	ip, _ := ctx.Value(submissionIPKey{}).(string)
	if ip == "" {
		ip = "127.0.0.1"
	}
	var result mailstate.Submission
	if !scheduled.IsZero() {
		scheduler, ok := j.submitter.(mailstate.ScheduledSubmitter)
		if !ok {
			return fail("forbiddenToSend")
		}
		result, err = scheduler.SubmitAt(ctx, user, ip, mid, from, to, blob.Data, scheduled)
	} else {
		result, err = j.submitter.Submit(ctx, user, ip, mid, from, to, blob.Data)
	}
	if err != nil {
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) && smtpErr.Code >= 500 {
			return fail("forbiddenToSend")
		}
		return fail("serverFail")
	}
	return result, nil
}
func (j *JMAPServer) submissionSet(ctx context.Context, user string, args map[string]interface{}, id string) MethodResponse {
	if j.submitter == nil {
		return methodError("unknownMethod", id)
	}
	for key := range args {
		switch key {
		case "accountId", "ifInState", "create", "update", "destroy", "onSuccessUpdateEmail", "onSuccessDestroyEmail":
		default:
			return methodError("invalidArguments", id)
		}
	}
	create, update := map[string]interface{}{}, map[string]interface{}{}
	for key, target := range map[string]*map[string]interface{}{"create": &create, "update": &update} {
		if v := args[key]; v != nil {
			m, ok := v.(map[string]interface{})
			if !ok {
				return methodError("invalidArguments", id)
			}
			*target = m
		}
	}
	destroy := []string{}
	if v := args["destroy"]; v != nil {
		destroy = toStringSlice(v)
		if destroy == nil {
			return methodError("invalidArguments", id)
		}
	}
	if len(create)+len(update)+len(destroy) > maxJMAPObjects {
		return methodError("tooManyObjectsInSet", id)
	}
	successUpdates := map[string]interface{}{}
	if v := args["onSuccessUpdateEmail"]; v != nil {
		var ok bool
		successUpdates, ok = v.(map[string]interface{})
		if !ok {
			return methodError("invalidArguments", id)
		}
		for _, p := range successUpdates {
			if _, ok := p.(map[string]interface{}); !ok {
				return methodError("invalidArguments", id)
			}
		}
	}
	successDestroy := []string{}
	if v := args["onSuccessDestroyEmail"]; v != nil {
		successDestroy = toStringSlice(v)
		if successDestroy == nil {
			return methodError("invalidArguments", id)
		}
	}
	if len(successUpdates)+len(successDestroy) > maxJMAPObjects {
		return methodError("tooManyObjectsInSet", id)
	}
	if (len(successUpdates)+len(successDestroy) > 0) && !j.emailMutations() {
		return methodError("invalidArguments", id)
	}
	j.submissionMu.Lock()
	defer j.submissionMu.Unlock()
	list, err := j.submitter.Submissions(ctx, user)
	if err != nil {
		return methodError("serverFail", id)
	}
	old, err := j.rememberSubmissions(ctx, user, list)
	if err != nil {
		return methodError("serverFail", id)
	}
	if v := args["ifInState"]; v != nil {
		since, ok := v.(string)
		if !ok {
			return methodError("invalidArguments", id)
		}
		if since != old {
			return methodError("stateMismatch", id)
		}
	}
	created, nc, nu, nd := map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}
	successful := map[string]string{}
	keys := []string{}
	for key := range create {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s, e := j.createSubmission(ctx, user, create[key])
		if e != nil {
			nc[key] = e
		} else {
			list = append(list, s)
			successful["#"+key] = s.EmailID
			successful[s.ID] = s.EmailID
			created[key] = map[string]interface{}{"id": s.ID, "sendAt": s.SendAt, "undoStatus": s.UndoStatus, "deliveryStatus": nil, "threadId": s.ThreadID, "dsnBlobIds": []string{}, "mdnBlobIds": []string{}}
		}
	}
	owned := map[string]string{}
	for _, s := range list {
		owned[s.ID] = s.EmailID
	}
	updated := map[string]interface{}{}
	for key, patch := range update {
		kind := "notFound"
		if owned[key] != "" {
			kind = "invalidProperties"
			fields, ok := patch.(map[string]interface{})
			if ok && len(fields) == 1 && fields["undoStatus"] == "canceled" {
				kind = "cannotUnsend"
				if manager, ok := j.submitter.(mailstate.ScheduledSubmitter); ok {
					err := manager.CancelSubmission(ctx, user, key)
					if err == nil {
						updated[key] = nil
						successful[key] = owned[key]
						continue
					}
					if !errors.Is(err, mailstate.ErrCannotUnsend) {
						kind = "serverFail"
					}
				}
			}
		}
		nu[key] = map[string]interface{}{"type": kind}
	}
	destroyed := []string{}
	removed := map[string]bool{}
	for _, key := range destroy {
		if removed[key] {
			continue
		}
		kind := "notFound"
		if owned[key] != "" {
			kind = "forbidden"
			if manager, ok := j.submitter.(mailstate.SubmissionManager); ok {
				if err := manager.DestroySubmission(ctx, user, key); err != nil {
					kind = "serverFail"
					if errors.Is(err, mailstate.ErrCannotUnsend) {
						kind = "forbidden"
					}
				} else {
					destroyed = append(destroyed, key)
					removed[key] = true
					successful[key] = owned[key]
					continue
				}
			}
		}
		nd[key] = map[string]interface{}{"type": kind}
	}
	remaining := []mailstate.Submission{}
	for _, r := range list {
		if !removed[r.ID] {
			if _, ok := updated[r.ID]; ok {
				r.UndoStatus = "canceled"
			}
			remaining = append(remaining, r)
		}
	}
	newState := submissionState(user, remaining)
	// Acceptance/deletion has already committed: history failure cannot turn it
	// into a method error that invites an accidental duplicate send.
	if _, err := j.rememberSubmissions(ctx, user, remaining); err != nil && j.logger != nil {
		j.logger.Warn("Submission history checkpoint failed after committed mutation", zap.Error(err))
	}

	response := MethodResponse{Name: "EmailSubmission/set", CallID: id, Arguments: map[string]interface{}{"accountId": "primary", "oldState": old, "newState": newState, "created": created, "notCreated": nc, "updated": updated, "notUpdated": nu, "destroyed": destroyed, "notDestroyed": nd}}

	if args["onSuccessUpdateEmail"] != nil || args["onSuccessDestroyEmail"] != nil {
		updates := map[string]interface{}{}
		remove := []interface{}{}
		hookKeys := []string{}
		for key := range successUpdates {
			hookKeys = append(hookKeys, key)
		}
		sort.Strings(hookKeys)
		for _, key := range hookKeys {
			if mid := successful[key]; mid != "" {
				merged, ok := updates[mid].(map[string]interface{})
				if !ok {
					merged = map[string]interface{}{}
				}
				for property, value := range successUpdates[key].(map[string]interface{}) {
					if old, exists := merged[property]; exists && !reflect.DeepEqual(old, value) {
						merged["conflictingSuccessHooks"] = true
					}
					merged[property] = value
				}
				updates[mid] = merged
			}
		}
		for _, key := range successDestroy {
			if mid := successful[key]; mid != "" {
				remove = append(remove, mid)
			}
		}
		response.Followups = []MethodResponse{j.processMethodCall(ctx, user, MethodCall{Name: "Email/set", ID: id, Arguments: map[string]interface{}{"update": updates, "destroy": remove}})}
	}
	return response
}
