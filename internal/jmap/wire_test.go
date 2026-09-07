package jmap

import (
	"encoding/json"
	"testing"
)

func TestInvocationWireArrays(t *testing.T) {
	var call MethodCall
	if err := json.Unmarshal([]byte(`["Email/get",{"accountId":"primary"},"a"]`), &call); err != nil || call.Name != "Email/get" || call.ID != "a" {
		t.Fatal(call, err)
	}
	encoded, err := json.Marshal(MethodResponse{Name: "Email/get", Arguments: map[string]interface{}{"state": "1"}, CallID: "a"})
	if err != nil || string(encoded) != `["Email/get",{"state":"1"},"a"]` {
		t.Fatal(string(encoded), err)
	}
	for _, raw := range []string{`{"0":"Email/get","1":{},"2":"a"}`, `["Email/get",{},"a",4]`, `["Email/get",null,"a"]`, `["Email/get",{},null]`, `["Email/get",{},7]`} {
		if err := json.Unmarshal([]byte(raw), &call); err == nil {
			t.Fatal("accepted malformed invocation", raw)
		}
	}
}
