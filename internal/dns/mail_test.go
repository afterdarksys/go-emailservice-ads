package dns

import (
	"context"
	"errors"
	mdns "github.com/miekg/dns"
	"go.uber.org/zap"
	"net"
	"testing"
	"time"
)

func TestMailDNSOutcomes(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &mdns.Server{PacketConn: conn, Handler: mdns.HandlerFunc(func(w mdns.ResponseWriter, q *mdns.Msg) {
		m := new(mdns.Msg)
		m.SetReply(q)
		question := q.Question[0]
		switch question.Name {
		case "temporary.test.":
			m.Rcode = mdns.RcodeServerFailure
		case "missing.test.":
			m.Rcode = mdns.RcodeNameError
		case "null.test.":
			rr, _ := mdns.NewRR("null.test. 60 IN MX 0 .")
			m.Answer = []mdns.RR{rr}
		case "implicit.test.":
			if question.Qtype == mdns.TypeA {
				rr, _ := mdns.NewRR("implicit.test. 60 IN A 192.0.2.1")
				m.Answer = []mdns.RR{rr}
			}
		case "explicit.test.":
			rr, _ := mdns.NewRR("explicit.test. 60 IN MX 10 mx.example.test.")
			m.Answer = []mdns.RR{rr}
		}
		w.WriteMsg(m)
	})}
	go server.ActivateAndServe()
	defer server.Shutdown()
	r := NewResolver(zap.NewNop())
	r.mailServers = []string{conn.LocalAddr().String()}
	r.timeout = time.Second
	for _, tc := range []struct {
		name               string
		permanent, success bool
	}{{"temporary.test", false, false}, {"missing.test", true, false}, {"null.test", true, false}, {"empty.test", true, false}, {"implicit.test", false, true}, {"explicit.test", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			mx, err := r.LookupMailMX(context.Background(), tc.name)
			if tc.success {
				if err != nil || len(mx) != 1 {
					t.Fatal(mx, err)
				}
				return
			}
			if err == nil {
				t.Fatal("failure accepted")
			}
			var e *MailDNSError
			permanent := errors.As(err, &e) && e.Permanent
			if permanent != tc.permanent {
				t.Fatal(err)
			}
		})
	}
}
