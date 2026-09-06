package ipfilter

import (
	"context"
	"net"
	"testing"
)

func TestLists(t *testing.T) {
	for _, tc := range []struct {
		name, ip string
		c        Config
		want     int
	}{
		{"disabled", "unknown", Config{}, 0},
		{"deny wins", "192.0.2.1", Config{Allowlist: []string{"192.0.2.1"}, Denylist: []string{"192.0.2.0/24"}}, 554},
		{"IPv6 denied", "2001:db8::1", Config{Denylist: []string{"2001:db8::/32"}}, 554},
		{"mapped IPv4", "::ffff:192.0.2.1", Config{Denylist: []string{"192.0.2.1"}}, 554},
		{"allow bypasses DNS", "2001:db8::1", Config{Allowlist: []string{"2001:db8::/32"}, RBLZones: []string{"rbl.example"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := Check(context.Background(), tc.c, tc.ip, func(context.Context, string) ([]string, error) { t.Fatal("unexpected DNS lookup"); return nil, nil })
			if code != tc.want {
				t.Fatalf("got %d want %d", code, tc.want)
			}
		})
	}
}

func TestDNSResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		answers    []string
		err        error
		deferError bool
		want       int
	}{
		{"listed", []string{"127.0.0.2"}, nil, false, 554},
		{"unlisted", nil, &net.DNSError{IsNotFound: true}, true, 0},
		{"timeout fail open", nil, context.DeadlineExceeded, false, 0},
		{"timeout defer", nil, context.DeadlineExceeded, true, 451},
		{"provider error", []string{"127.255.255.254"}, nil, true, 451},
		{"unexpected address", []string{"203.0.113.1"}, nil, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := Check(context.Background(), Config{RBLZones: []string{"rbl.example"}, DeferOnError: tc.deferError}, "192.0.2.1", func(ctx context.Context, q string) ([]string, error) {
				if q != "1.2.0.192.rbl.example." {
					t.Fatalf("query %s", q)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				return tc.answers, tc.err
			})
			if code != tc.want {
				t.Fatalf("got %d want %d", code, tc.want)
			}
		})
	}
}

func TestIPv6Query(t *testing.T) {
	Check(context.Background(), Config{RBLZones: []string{"rbl.example."}}, "2001:db8::1", func(_ context.Context, q string) ([]string, error) {
		if q != "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.rbl.example." {
			t.Fatalf("query %s", q)
		}
		return nil, nil
	})
}

func TestValidation(t *testing.T) {
	for _, c := range []Config{{Denylist: []string{"bad"}}, {Allowlist: []string{"192.0.2.1/99"}}, {Timeout: "0s"}, {Timeout: "bad"}, {RBLZones: []string{"bad zone"}}, {RBLZones: []string{""}}} {
		if c.Validate() == nil {
			t.Fatalf("accepted invalid config: %+v", c)
		}
	}
	if err := (Config{Allowlist: []string{"::1", "192.0.2.0/24"}, RBLZones: []string{"rbl.example."}, Timeout: "2s"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
