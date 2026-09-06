package smtpd

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/filtering"
	"golang.org/x/time/rate"
)

type quotaBucket struct {
	limiter *rate.Limiter
	last    time.Time
}
type abuseLimits struct {
	mu      sync.Mutex
	buckets map[string]*quotaBucket
}

func (l *abuseLimits) allow(user, domain string, count, userMax, domainMax int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, b := range l.buckets {
		if now.Sub(b.last) > 2*time.Hour {
			delete(l.buckets, k)
		}
	}
	var buckets []*quotaBucket
	for _, spec := range []struct {
		key string
		max int
	}{{"user:" + user, userMax}, {"domain:" + domain, domainMax}} {
		if spec.max <= 0 {
			continue
		}
		b := l.buckets[spec.key]
		if b == nil {
			b = &quotaBucket{limiter: rate.NewLimiter(rate.Limit(float64(spec.max)/3600), spec.max)}
			l.buckets[spec.key] = b
		}
		b.last = now
		if b.limiter.TokensAt(now) < float64(count) {
			return false
		}
		buckets = append(buckets, b)
	}
	for _, b := range buckets {
		b.limiter.AllowN(now, count)
	}
	return true
}
func (qm *QueueManager) Running() bool { return qm.ctx != nil && qm.ctx.Err() == nil }
func (qm *QueueManager) ReleaseHeld(ctx context.Context, id, actor string) error {
	entry, err := qm.store.Get(id)
	if err != nil {
		return err
	}
	if entry.Status != "held" {
		return fmt.Errorf("message is not held")
	}
	if qm.platform.ScannerURL == "" {
		return fmt.Errorf("quarantine release requires a configured scanner")
	}
	ctx, cancel := context.WithTimeout(ctx, configuredDuration(qm.platform.ScannerTimeout, 15*time.Second))
	defer cancel()
	if err := scanMalware(ctx, qm.platform, entry.Data); err != nil {
		return err
	}
	verdict, err := filtering.Scan(ctx, qm.platform.ScannerURL, entry.From, entry.Metadata["client_ip"], entry.To, entry.Data)
	if err != nil {
		return err
	}
	if verdict.Action != "no action" {
		return fmt.Errorf("rescan did not clear message: %s", verdict.Action)
	}
	return qm.store.ReleaseHeld(id, actor)
}

func (qm *QueueManager) enqueueTLSReport(ctx context.Context, to string, raw []byte) error {
	addr, err := mail.ParseAddress(to)
	if err != nil || addr.Address != to || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("invalid TLS report recipient")
	}
	b := base64.StdEncoding.EncodeToString(raw)
	var wrapped strings.Builder
	for len(b) > 76 {
		wrapped.WriteString(b[:76] + "\r\n")
		b = b[76:]
	}
	wrapped.WriteString(b + "\r\n")
	boundary := generateTraceID()
	body := fmt.Sprintf("From: postmaster@%s\r\nTo: %s\r\nDate: %s\r\nSubject: SMTP TLS report\r\nMIME-Version: 1.0\r\nContent-Type: multipart/report; report-type=tlsrpt; boundary=%q\r\n\r\n--%s\r\nContent-Type: text/plain\r\n\r\nSMTP TLS aggregate report attached.\r\n--%s\r\nContent-Type: application/tlsrpt+json\r\nContent-Disposition: attachment; filename=tls-report.json\r\nContent-Transfer-Encoding: base64\r\n\r\n%s--%s--\r\n", qm.hostname, to, time.Now().Format(time.RFC1123Z), boundary, boundary, boundary, wrapped.String(), boundary)
	return qm.Enqueue(&Message{From: "", To: []string{to}, Data: []byte(body), Tier: TierEmergency, IsBounce: true, IsTLSReport: true})
}

func (qm *QueueManager) Ready(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !qm.Running() {
		return false
	}
	if qm.platform.PolicyRequired && qm.policyManager == nil {
		return false
	}
	if qm.platform.MalwareRequired {
		if err := filtering.ClamAVHealth(ctx, qm.platform.ClamAVAddress); err != nil {
			return false
		}
	}
	endpoints := []string{}
	if qm.platform.ScannerRequired {
		endpoints = append(endpoints, strings.TrimRight(qm.platform.ScannerURL, "/")+"/ping")
	}
	if qm.platform.ReputationRequired && qm.reputationDB != nil {
		if qm.reputationDB.Health(ctx) != nil {
			return false
		}
	}
	if qm.platform.ReputationRequired && qm.reputationDB == nil {
		endpoints = append(endpoints, strings.TrimRight(qm.platform.ReputationURL, "/")+"/health")
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return false
		}
	}
	return true
}
