package security

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestVerifyDMARCUsesOrganizationalDomainPolicy(t *testing.T) {
	tests := []struct {
		name       string
		record     string
		wantPolicy DMARCPolicy
	}{
		{
			name:       "omitted subdomain policy inherits p",
			record:     "v=DMARC1; p=reject",
			wantPolicy: DMARCPolicyReject,
		},
		{
			name:       "explicit subdomain policy overrides p",
			record:     "v=DMARC1; p=reject; sp=quarantine",
			wantPolicy: DMARCPolicyQuarantine,
		},
		{
			name:       "explicit sp none does not inherit p",
			record:     "v=DMARC1; p=reject; sp=none",
			wantPolicy: DMARCPolicyNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookups := make([]string, 0, 2)
			engine := &PolicyEngine{
				logger: zap.NewNop(),
				lookupTXT: func(_ context.Context, name string) ([]string, error) {
					lookups = append(lookups, name)
					if name == "_dmarc.example.com" {
						return []string{tt.record}, nil
					}
					return nil, nil
				},
			}

			result, policy, err := engine.VerifyDMARC(context.Background(), "news.example.com", "mail.invalid", SPFFail, nil)
			if err != nil {
				t.Fatalf("VerifyDMARC() error = %v", err)
			}
			if result != DMARCFail {
				t.Fatalf("VerifyDMARC() result = %q, want fail", result)
			}
			if policy != tt.wantPolicy {
				t.Fatalf("VerifyDMARC() policy = %q, want %q", policy, tt.wantPolicy)
			}
			if len(lookups) != 2 || lookups[0] != "_dmarc.news.example.com" || lookups[1] != "_dmarc.example.com" {
				t.Fatalf("DMARC lookups = %v, want exact then organizational domain", lookups)
			}
		})
	}
}

func TestEvaluateDMARCReturnsPct(t *testing.T) {
	engine := &PolicyEngine{
		logger: zap.NewNop(),
		lookupTXT: func(_ context.Context, name string) ([]string, error) {
			if name == "_dmarc.example.com" {
				return []string{"v=DMARC1; p=reject; pct=25"}, nil
			}
			return nil, nil
		},
	}

	result, policy, pct, err := engine.EvaluateDMARC(context.Background(), "example.com", "mail.invalid", SPFFail, nil)
	if err != nil || result != DMARCFail || policy != DMARCPolicyReject || pct != 25 {
		t.Fatalf("EvaluateDMARC() = (%q, %q, %d, %v), want (fail, reject, 25, nil)", result, policy, pct, err)
	}
}
