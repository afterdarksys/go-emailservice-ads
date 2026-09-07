package delivery

import "testing"

func TestTransportSpecificityAndSender(t *testing.T) {
	d := &MailDelivery{routes: []Route{{Domain: "*", NextHops: []NextHop{{Address: "fallback:25"}}}, {Domain: "*.example.test", NextHops: []NextHop{{Address: "suffix:25"}}}, {Domain: "mx.example.test", NextHops: []NextHop{{Address: "exact:25"}}}, {Domain: "mx.example.test", SenderDomain: "sender.test", NextHops: []NextHop{{Address: "sender:25"}}}}}
	for _, v := range []struct{ domain, from, want string }{{"mx.example.test", "a@sender.test", "sender:25"}, {"mx.example.test", "a@other.test", "exact:25"}, {"a.example.test", "", "suffix:25"}, {"example.test", "", "fallback:25"}, {"evil-example.test", "", "fallback:25"}} {
		if got := d.selectRoute(v.domain, v.from); len(got) != 1 || got[0].Address != v.want {
			t.Fatal(v, got)
		}
	}
	if err := ValidateRoutes(d.routes); err != nil {
		t.Fatal(err)
	}
}
