package smtpd

import "testing"

func TestSPFIdentityForMailFrom(t *testing.T) {
	tests := []struct {
		name         string
		from, ehlo   string
		wantDomain   string
		wantIdentity string
	}{
		{"ordinary sender", "sender@example.com", "client.example", "example.com", "sender@example.com"},
		{"null reverse path", "", "client.example", "client.example", "postmaster@client.example"},
		{"smtp null reverse path", "<>", "client.example", "client.example", "postmaster@client.example"},
		{"missing identity", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, identity := spfIdentityForMailFrom(tt.from, tt.ehlo)
			if domain != tt.wantDomain || identity != tt.wantIdentity {
				t.Fatalf("spfIdentityForMailFrom() = (%q, %q), want (%q, %q)", domain, identity, tt.wantDomain, tt.wantIdentity)
			}
		})
	}
}

func TestDMARCPolicyApplies(t *testing.T) {
	message := []byte("From: sender@example.com\r\n\r\nbody")
	if dmarcPolicyApplies(message, 0) {
		t.Fatal("pct=0 must not apply DMARC policy")
	}
	if !dmarcPolicyApplies(message, 100) {
		t.Fatal("pct=100 must apply DMARC policy")
	}
	first := dmarcPolicyApplies(message, 50)
	if dmarcPolicyApplies(message, 50) != first {
		t.Fatal("pct sampling must be stable for the same message")
	}
}
