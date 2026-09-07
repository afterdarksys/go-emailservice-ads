package jmap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func (j *JMAPServer) acquirePush(user string) (func(), bool) {
	j.pushMu.Lock()
	defer j.pushMu.Unlock()
	if j.pushUsers == nil {
		j.pushUsers = map[string]int{}
	}
	if j.pushTotal >= 64 || j.pushUsers[user] >= 4 {
		return nil, false
	}
	j.pushTotal++
	j.pushUsers[user]++
	return func() {
		j.pushMu.Lock()
		defer j.pushMu.Unlock()
		j.pushTotal--
		j.pushUsers[user]--
		if j.pushUsers[user] == 0 {
			delete(j.pushUsers, user)
		}
	}, true
}

func (j *JMAPServer) pushState(ctx context.Context, user string) (map[string]string, error) {
	owned, email, err := j.emailSnapshot(ctx, user)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for id := range owned {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b, _ := json.Marshal(ids)
	states := map[string]string{"Email": email, "EmailDelivery": fmt.Sprintf("d1:%x", sha256.Sum256(b))}
	_, thread, err := j.threadSnapshot(ctx, user)
	if err != nil {
		return nil, err
	}
	states["Thread"] = thread
	if s, ok := j.store.(mailstate.MailboxStore); ok {
		_, state, err := s.MailboxSnapshot(ctx, user)
		if err != nil {
			return nil, err
		}
		states["Mailbox"] = state
	}
	if j.submitter != nil {
		list, err := j.submitter.Submissions(ctx, user)
		if err != nil {
			return nil, err
		}
		state, err := j.rememberSubmissions(ctx, user, list)
		if err != nil {
			return nil, err
		}
		states["EmailSubmission"] = state
	}
	return states, nil
}

func (j *JMAPServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", 405)
		return
	}
	user := authUserFromContext(r.Context())
	if user == "" {
		http.Error(w, "Authentication required", 401)
		return
	}
	q := r.URL.Query()
	types := q.Get("types")
	closeAfter := q.Get("closeafter")
	ping, err := strconv.Atoi(q.Get("ping"))
	if types == "" || (closeAfter != "no" && closeAfter != "state") || err != nil || ping < 0 {
		http.Error(w, "Invalid event parameters", 400)
		return
	}
	wanted := map[string]bool{}
	for _, kind := range strings.Split(types, ",") {
		switch kind {
		case "*", "Email", "EmailDelivery", "Thread", "Mailbox", "EmailSubmission":
			wanted[kind] = true
		default:
			http.Error(w, "Unsupported event type", 400)
			return
		}
	}
	if wanted["*"] && len(wanted) != 1 {
		http.Error(w, "Invalid event types", 400)
		return
	}
	if ping > 0 && ping < 30 {
		ping = 30
	}
	if ping > 300 {
		ping = 300
	}
	release, ok := j.acquirePush(user)
	if !ok {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "Too many event streams", 429)
		return
	}
	defer release()
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	// Writes have a fresh deadline; idle streams are allowed to outlive the normal
	// API timeout. A stalled reader cannot hold a handler indefinitely.
	write := func(event, token string, data interface{}) bool {
		if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		b, _ := json.Marshal(data)
		if token != "" {
			if _, err := fmt.Fprintf(w, "id: %s\n", token); err != nil {
				return false
			}
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	last := r.Header.Get("Last-Event-ID")
	lastSent := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if j.validator != nil {
			u, ok := j.validator.GetUserStore().GetUser(user)
			if !ok || !u.Enabled {
				return
			}
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				if subject, valid := j.bearerSubject(header); !valid || subject != user {
					return
				}
			}
		}
		states, err := j.pushState(r.Context(), user)
		if err != nil {
			return
		}
		selected := map[string]string{}
		for kind, state := range states {
			if wanted["*"] || wanted[kind] {
				selected[kind] = state
			}
		}
		b, _ := json.Marshal(selected)
		token := fmt.Sprintf("p1:%x", sha256.Sum256(append([]byte(user+"\x00"), b...)))
		if token != last {
			if !write("state", token, map[string]interface{}{"@type": "StateChange", "changed": map[string]interface{}{"primary": selected}}) {
				return
			}
			last = token
			lastSent = time.Now()
			if closeAfter == "state" {
				return
			}
		} else if ping > 0 && time.Since(lastSent) >= time.Duration(ping)*time.Second {
			if !write("ping", "", map[string]int{"interval": ping}) {
				return
			}
			lastSent = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-j.pushDone:
			return
		case <-ticker.C:
		}
	}
}
