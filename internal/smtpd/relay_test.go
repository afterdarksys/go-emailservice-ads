package smtpd

import (
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
)

func newRelayTestSession(t *testing.T, localDomains, allowedNetworks []string) *Session {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.LocalDomains = localDomains
	cfg.Server.Relay.AllowedNetworks = allowedNetworks
	return &Session{config: cfg}
}

// Threats: an open relay lets any internet sender use this server to deliver
// mail to arbitrary third parties. These tests prove the default posture is
// deny, and that permission requires an explicit, positive grant.

func TestIsRelayPermittedDeniesUnauthenticatedNonLocalByDefault(t *testing.T) {
	s := newRelayTestSession(t, []string{"gomeow.media"}, nil)
	s.authenticated = false
	s.relayAuthIP = false

	if s.isRelayPermitted("victim@some-other-domain.test") {
		t.Fatal("isRelayPermitted() = true, want false: unauthenticated non-local relay must be denied by default")
	}
}

func TestIsRelayPermittedDeniesEvenWithTrustedFilterFlag(t *testing.T) {
	// trustedFilter marks a connection as coming through the MailScript
	// perimeter proxy (inbound filtering already applied) — it must never by
	// itself grant relay to third-party domains.
	s := newRelayTestSession(t, []string{"gomeow.media"}, nil)
	s.trustedFilter = true

	if s.isRelayPermitted("victim@some-other-domain.test") {
		t.Fatal("isRelayPermitted() = true, want false: trustedFilter must not imply relay authorization")
	}
}

func TestIsRelayPermittedAllowsLocalDomainRegardlessOfAuth(t *testing.T) {
	s := newRelayTestSession(t, []string{"gomeow.media"}, nil)
	s.authenticated = false
	s.relayAuthIP = false

	if !s.isRelayPermitted("user@gomeow.media") {
		t.Fatal("isRelayPermitted() = false, want true: local-domain delivery is normal inbound mail, not relay")
	}
}

func TestIsRelayPermittedAllowsLocalDomainCaseInsensitively(t *testing.T) {
	s := newRelayTestSession(t, []string{"gomeow.media"}, nil)

	if !s.isRelayPermitted("user@GoMeow.Media") {
		t.Fatal("isRelayPermitted() = false, want true: local domain match must be case-insensitive")
	}
}

func TestIsRelayPermittedAllowsAuthenticatedSenderToRelay(t *testing.T) {
	s := newRelayTestSession(t, []string{"gomeow.media"}, nil)
	s.authenticated = true

	if !s.isRelayPermitted("recipient@anywhere-else.test") {
		t.Fatal("isRelayPermitted() = false, want true: authenticated senders may relay")
	}
}

func TestIsRelayPermittedAllowsRelayAuthorizedIP(t *testing.T) {
	s := newRelayTestSession(t, []string{"gomeow.media"}, []string{"10.0.0.0/8"})
	s.authenticated = false
	s.relayAuthIP = true // set by Backend.isRelayAuthorizedNetwork in NewSession

	if !s.isRelayPermitted("recipient@anywhere-else.test") {
		t.Fatal("isRelayPermitted() = false, want true: relay-authorized network IP may relay")
	}
}

func TestIsRelayAuthorizedNetworkFailsClosedWithNoConfig(t *testing.T) {
	b := &Backend{config: &config.Config{}}
	if b.isRelayAuthorizedNetwork("10.1.2.3") {
		t.Fatal("isRelayAuthorizedNetwork() = true, want false: empty AllowedNetworks must deny everyone")
	}
}

func TestIsRelayAuthorizedNetworkMatchesConfiguredCIDR(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Relay.AllowedNetworks = []string{"10.0.0.0/8"}
	b := &Backend{config: cfg}

	if !b.isRelayAuthorizedNetwork("10.1.2.3") {
		t.Fatal("isRelayAuthorizedNetwork(10.1.2.3) = false, want true: address is inside 10.0.0.0/8")
	}
	if b.isRelayAuthorizedNetwork("203.0.113.7") {
		t.Fatal("isRelayAuthorizedNetwork(203.0.113.7) = true, want false: address is outside every configured network")
	}
}

func TestIsRelayAuthorizedNetworkRejectsUnparseableIP(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Relay.AllowedNetworks = []string{"10.0.0.0/8"}
	b := &Backend{config: cfg}

	if b.isRelayAuthorizedNetwork("not-an-ip") {
		t.Fatal("isRelayAuthorizedNetwork() = true, want false: unparseable IP must fail closed")
	}
}
