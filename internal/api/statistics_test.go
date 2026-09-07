package api

import (
	"github.com/afterdarksys/go-emailservice-ads/internal/smtpd"
	"strings"
	"testing"
)

func TestOperationalStatisticsUseRegisteredListenerAndScopes(t *testing.T) {
	s, _ := newMailboxTestServer(t, false)
	s.qm = &smtpd.QueueManager{}
	s.qm.RegisterStatistics("127.0.0.1:2525", func() map[string]interface{} {
		return map[string]interface{}{"dns": map[string]int{"hits": 17}, "greylisting": map[string]bool{"enabled": false}}
	})
	r := doMailboxRequest(s, "GET", "/api/v1/dns/stats", testAPIKey, nil)
	if r.Code != 403 {
		t.Fatal(r.Code)
	}
	s.config.API.APIKeys[0].Permissions = []string{"dns:read"}
	r = doMailboxRequest(s, "GET", "/api/v1/dns/stats", testAPIKey, nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "17") || strings.Contains(r.Body.String(), "authentication") {
		t.Fatal(r.Code, r.Body.String())
	}
}
