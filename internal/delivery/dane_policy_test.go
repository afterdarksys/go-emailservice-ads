package delivery

import (
	"errors"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/security/dane"
)

func TestDANETLSRequirement(t *testing.T) {
	tests := []struct {
		name      string
		result    *dane.TLSALookupResult
		lookupErr error
		mandatory bool
		wantErr   bool
	}{
		{
			name:      "authenticated TLSA records require DANE",
			result:    &dane.TLSALookupResult{DNSSECValid: true, Records: []*dane.TLSARecord{{}}},
			mandatory: true,
		},
		{
			name:   "authenticated absence permits opportunistic TLS",
			result: &dane.TLSALookupResult{DNSSECValid: true},
		},
		{
			name:   "insecure TLSA records do not assert DANE",
			result: &dane.TLSALookupResult{DNSSECInsecure: true, Records: []*dane.TLSARecord{{}}},
		},
		{
			name:      "lookup error defers delivery",
			lookupErr: errors.New("DNS timeout"),
			wantErr:   true,
		},
		{
			name:    "bogus DNSSEC defers delivery",
			result:  &dane.TLSALookupResult{DNSSECBogus: true, ErrorReason: "bad signature"},
			wantErr: true,
		},
		{
			name:    "empty result defers delivery",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mandatory, err := daneTLSRequirement(tt.result, tt.lookupErr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("daneTLSRequirement() error = %v, wantErr %v", err, tt.wantErr)
			}
			if mandatory != tt.mandatory {
				t.Fatalf("daneTLSRequirement() mandatory = %v, want %v", mandatory, tt.mandatory)
			}
		})
	}
}
