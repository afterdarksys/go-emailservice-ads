package jmap

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/mailstate"
)

func TestQueryAnchorPagination(t *testing.T) {
	j, store, _ := compositionServer(t)
	ctx := context.Background()
	sub := &recordingSubmitter{records: map[string][]mailstate.Submission{}}
	j.SetSubmitter(sub)
	for _, subject := range []string{"a", "b", "c"} {
		mid, err := store.StoreMessage(ctx, "alice", "INBOX", []byte("Subject: "+subject+"\r\n\r\nbody"))
		if err != nil {
			t.Fatal(err)
		}
		sub.records["alice"] = append(sub.records["alice"], mailstate.Submission{ID: subject, EmailID: mid, SendAt: "2026-09-07T00:00:00Z"})
	}
	for _, method := range []string{"Email/query", "EmailSubmission/query"} {
		t.Run(method, func(t *testing.T) {
			call := func(user string, args map[string]interface{}) MethodResponse {
				return j.processMethodCall(ctx, user, MethodCall{Name: method, ID: "page", Arguments: args})
			}
			full := call("alice", nil)
			ids := full.Arguments["ids"].([]string)
			if len(ids) != 3 {
				t.Fatal(full)
			}
			cases := []struct {
				name     string
				args     map[string]interface{}
				position int
				want     []string
				kind     string
			}{
				{"anchor", map[string]interface{}{"anchor": ids[1]}, 1, ids[1:], ""},
				{"preceding", map[string]interface{}{"anchor": ids[1], "anchorOffset": float64(-1), "limit": float64(2)}, 0, ids[:2], ""},
				{"following", map[string]interface{}{"anchor": ids[1], "anchorOffset": float64(1)}, 2, ids[2:], ""},
				{"clamp", map[string]interface{}{"anchor": ids[1], "anchorOffset": float64(-100)}, 0, ids, ""},
				{"past end", map[string]interface{}{"anchor": ids[1], "anchorOffset": float64(100)}, 101, []string{}, ""},
				{"ignore position", map[string]interface{}{"anchor": ids[1], "position": "ignored"}, 1, ids[1:], ""},
				{"ignore offset", map[string]interface{}{"anchorOffset": "ignored"}, 0, ids, ""},
				{"negative position", map[string]interface{}{"position": float64(-1)}, 2, ids[2:], ""},
				{"negative clamp", map[string]interface{}{"position": float64(-100)}, 0, ids, ""},
				{"position beyond end", map[string]interface{}{"position": float64(100)}, 100, []string{}, ""},
				{"zero limit", map[string]interface{}{"anchor": ids[1], "limit": float64(0)}, 1, []string{}, ""},
				{"null limit", map[string]interface{}{"anchor": nil, "limit": nil}, 0, ids, ""},
				{"missing", map[string]interface{}{"anchor": "missing"}, 0, nil, "anchorNotFound"},
				{"empty anchor", map[string]interface{}{"anchor": ""}, 0, nil, "invalidArguments"},
				{"bad anchor", map[string]interface{}{"anchor": true}, 0, nil, "invalidArguments"},
				{"fraction", map[string]interface{}{"anchor": ids[1], "anchorOffset": 0.5}, 0, nil, "invalidArguments"},
				{"overflow", map[string]interface{}{"anchor": ids[1], "anchorOffset": 1e30}, 0, nil, "invalidArguments"},
				{"negative limit", map[string]interface{}{"limit": float64(-1)}, 0, nil, "invalidArguments"},
				{"null position", map[string]interface{}{"position": nil}, 0, nil, "invalidArguments"},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := call("alice", tc.args)
					if tc.kind != "" {
						if r.Name != "error" || r.Arguments["type"] != tc.kind {
							t.Fatal(r)
						}
						return
					}
					if r.Name != method || r.Arguments["position"] != tc.position || !reflect.DeepEqual(r.Arguments["ids"], tc.want) || r.Arguments["queryState"] != full.Arguments["queryState"] {
						t.Fatal(r)
					}
				})
			}
			foreign := call("bob", map[string]interface{}{"anchor": ids[0]})
			if foreign.Arguments["type"] != "anchorNotFound" {
				t.Fatal(foreign)
			}
			filter := map[string]interface{}{"subject": "does not match"}
			if method == "EmailSubmission/query" {
				filter = map[string]interface{}{"emailIds": []interface{}{"missing"}}
			}
			filtered := call("alice", map[string]interface{}{"anchor": ids[0], "filter": filter})
			if filtered.Arguments["type"] != "anchorNotFound" {
				t.Fatal(filtered)
			}
		})
	}
}

func TestQueryPageNumericBoundaries(t *testing.T) {
	ids := []string{"a", "b"}
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 9007199254740992, -9007199254740992} {
		if _, _, _, kind := queryPage(map[string]interface{}{"position": n}, ids); kind != "invalidArguments" {
			t.Fatalf("accepted %v", n)
		}
	}
	many := make([]string, maxJMAPObjects+1)
	if _, start, end, kind := queryPage(map[string]interface{}{"limit": float64(maxJMAPObjects + 100)}, many); kind != "" || start != 0 || end != maxJMAPObjects {
		t.Fatal(start, end, kind)
	}
}
