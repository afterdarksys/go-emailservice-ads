package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DurableTLSReports keeps one independently acknowledged report per domain/day.
// A failed upload never clears a report or events from another domain/day.
type DurableTLSReports struct {
	SendMail func(context.Context, string, []byte) error
	dir, org string
	mu       sync.Mutex
	sendMu   sync.Mutex
	lookup   func(context.Context, string) ([]string, error)
	client   *http.Client
}

func NewDurableTLSReports(dir, org string) (*DurableTLSReports, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &DurableTLSReports{dir: dir, org: org, lookup: net.DefaultResolver.LookupTXT, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func atomicJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	f.Close()
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (t *DurableTLSReports) Record(domain, host, kind string, success bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := time.Now().UTC().Truncate(24 * time.Hour)
	id := fmt.Sprintf("%s-%x", start.Format("20060102"), sha256.Sum256([]byte(domain)))
	path := filepath.Join(t.dir, id+".json")
	report := TLSRPTReport{OrganizationName: t.org, ReportID: id, DateRange: DateRange{StartDatetime: start, EndDatetime: start.Add(24*time.Hour - time.Second)}}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(raw, &report); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	i := -1
	for j, p := range report.Policies {
		if p.Policy.PolicyType == kind {
			i = j
			break
		}
	}
	if i < 0 {
		report.Policies = append(report.Policies, PolicyReport{Policy: Policy{PolicyType: kind, PolicyDomain: domain, MXHost: []string{host}}})
		i = len(report.Policies) - 1
	}
	p := &report.Policies[i]
	if success {
		p.Summary.TotalSuccessfulSessionCount++
	} else {
		p.Summary.TotalFailureSessionCount++
		found := false
		for j := range p.FailureDetails {
			f := &p.FailureDetails[j]
			if f.ResultType == "validation-failure" && f.ReceivingMXHostname == host {
				f.FailedSessionCount++
				found = true
				break
			}
		}
		if !found {
			p.FailureDetails = append(p.FailureDetails, FailureDetails{ResultType: "validation-failure", ReceivingMXHostname: host, FailedSessionCount: 1})
		}
	}
	return atomicJSON(path, &report)
}
func (t *DurableTLSReports) SendPending(ctx context.Context) error {
	// Serialize senders so periodic and manual flushes cannot upload the same
	// report concurrently. Recording current-day events remains independent.
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	files, err := filepath.Glob(filepath.Join(t.dir, "*.json"))
	if err != nil {
		return err
	}
	var failures []string
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		var r TLSRPTReport
		if err = json.Unmarshal(raw, &r); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if r.DateRange.EndDatetime.After(time.Now().UTC()) || len(r.Policies) == 0 {
			continue
		}
		if err = t.send(ctx, r.Policies[0].Policy.PolicyDomain, raw); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if err = os.Remove(path); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("TLS report delivery: %s", strings.Join(failures, "; "))
	}
	return nil
}
func (t *DurableTLSReports) send(ctx context.Context, domain string, raw []byte) error {
	records, err := t.lookup(ctx, "_smtp._tls."+domain)
	if err != nil {
		return err
	}
	var destinations []string
	for _, r := range records {
		if !strings.HasPrefix(r, "v=TLSRPTv1;") {
			continue
		}
		for _, part := range strings.Split(r, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "rua=") {
				destinations = append(destinations, strings.Split(strings.TrimPrefix(part, "rua="), ",")...)
			}
		}
	}
	if len(destinations) == 0 {
		return fmt.Errorf("no TLS reporting destination for %s", domain)
	}
	for _, dest := range destinations {
		dest = strings.TrimSpace(dest)
		if strings.HasPrefix(dest, "mailto:") && t.SendMail != nil {
			if err := t.SendMail(ctx, strings.TrimPrefix(dest, "mailto:"), raw); err == nil {
				return nil
			}
			continue
		}
		if !strings.HasPrefix(dest, "https://") {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "POST", dest, bytes.NewReader(raw))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/tlsrpt+json")
		resp, err := t.client.Do(req)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
	}
	return fmt.Errorf("no HTTPS TLS reporting destination accepted report for %s", domain)
}
