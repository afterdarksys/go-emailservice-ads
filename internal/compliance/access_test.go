package compliance

import "testing"

func TestDomainAndActionIsolation(t *testing.T) {
	c := Config{EnforceDomainAccess: true, Access: []Grant{{Principal: "oauth:alice", Domains: []string{"a.test"}, Actions: []string{"read", "export"}}}}
	if !c.Authorized("oauth:alice", "a.test", "export") {
		t.Fatal("authorized export denied")
	}
	for _, tc := range []struct{ p, d, a string }{{"oauth:alice", "a.test,b.test", "export"}, {"oauth:bob", "a.test", "read"}, {"oauth:alice", "a.test", "release"}} {
		if c.Authorized(tc.p, tc.d, tc.a) {
			t.Fatal("access leaked", tc)
		}
	}
}
