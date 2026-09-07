package security

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type DMARCPolicySnapshot struct {
	Domain    string      `xml:"domain"`
	ADKIM     string      `xml:"adkim"`
	ASPF      string      `xml:"aspf"`
	Policy    DMARCPolicy `xml:"p"`
	SubPolicy DMARCPolicy `xml:"sp"`
	Pct       int         `xml:"pct"`
}
type DMARCEvaluation struct {
	Result                  DMARCResult
	Policy                  DMARCPolicy
	Pct                     int
	Published               DMARCPolicySnapshot
	RUA                     string
	SPFAligned, DKIMAligned bool
}
type DMARCAuthResult struct {
	Domain string `xml:"domain"`
	Scope  string `xml:"scope,omitempty"`
	Result string `xml:"result"`
}
type DMARCRow struct {
	Row struct {
		IP        string `xml:"source_ip"`
		Count     int64  `xml:"count"`
		Evaluated struct {
			Disposition string       `xml:"disposition"`
			DKIM        string       `xml:"dkim"`
			SPF         string       `xml:"spf"`
			Reason      *DMARCReason `xml:"reason,omitempty"`
		} `xml:"policy_evaluated"`
	} `xml:"row"`
	Identifiers struct {
		EnvelopeFrom string `xml:"envelope_from"`
		HeaderFrom   string `xml:"header_from"`
	} `xml:"identifiers"`
	Auth struct {
		DKIM []DMARCAuthResult `xml:"dkim"`
		SPF  DMARCAuthResult   `xml:"spf"`
	} `xml:"auth_results"`
}
type DMARCReason struct {
	Type    string `xml:"type"`
	Comment string `xml:"comment,omitempty"`
}
type DMARCReport struct {
	Sealed   bool     `xml:"-"`
	XMLName  xml.Name `xml:"feedback" json:"-"`
	Version  string   `xml:"version"`
	Metadata struct {
		Org   string `xml:"org_name"`
		Email string `xml:"email"`
		ID    string `xml:"report_id"`
		Range struct {
			Begin int64 `xml:"begin"`
			End   int64 `xml:"end"`
		} `xml:"date_range"`
	} `xml:"report_metadata"`
	Published DMARCPolicySnapshot `xml:"policy_published"`
	Records   []DMARCRow          `xml:"record"`
	RUA       string              `xml:"-"`
	Sent      map[string]bool     `xml:"-"`
}
type DurableDMARCReports struct {
	mu       sync.Mutex
	sendMu   sync.Mutex
	dir, org string
	lookup   func(context.Context, string) ([]string, error)
	SendMail func(context.Context, string, string, []byte) error
}

func NewDurableDMARCReports(dir, org string) (*DurableDMARCReports, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &DurableDMARCReports{dir: dir, org: org, lookup: net.DefaultResolver.LookupTXT}, nil
}
func (t *DurableDMARCReports) Record(at time.Time, e DMARCEvaluation, ip, headerFrom, spfDomain, spfResult, scope, disposition, reason string, dkim []DKIMVerification) error {
	if e.Published.Domain == "" || e.RUA == "" {
		return nil
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid DMARC source IP")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	start := at.UTC().Truncate(24 * time.Hour)
	policy, _ := json.Marshal([]interface{}{e.Published, e.RUA})
	id := fmt.Sprintf("%s-%x", start.Format("20060102"), sha256.Sum256(policy))
	baseID := id
	var path string
	var r DMARCReport
	for generation := 0; ; generation++ {
		if generation > 0 {
			id = fmt.Sprintf("%s-%d", baseID, generation)
		}
		path = filepath.Join(t.dir, id+".json")
		r = DMARCReport{Version: "1.0", Published: e.Published, RUA: e.RUA, Sent: map[string]bool{}}
		r.Metadata.Org = t.org
		r.Metadata.Email = "postmaster@" + t.org
		r.Metadata.ID = id
		r.Metadata.Range.Begin = start.Unix()
		r.Metadata.Range.End = start.Add(24*time.Hour - time.Second).Unix()
		if raw, err := os.ReadFile(path); err == nil {
			if err = json.Unmarshal(raw, &r); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if !r.Sealed && len(r.Sent) == 0 {
			break
		}
	}
	row := DMARCRow{}
	row.Row.IP = ip
	row.Row.Evaluated.Disposition = disposition
	row.Row.Evaluated.DKIM = "fail"
	row.Row.Evaluated.SPF = "fail"
	if e.SPFAligned {
		row.Row.Evaluated.SPF = "pass"
	}
	if e.DKIMAligned {
		row.Row.Evaluated.DKIM = "pass"
	}
	if reason != "" {
		row.Row.Evaluated.Reason = &DMARCReason{Type: reason}
	}
	row.Identifiers.HeaderFrom = headerFrom
	row.Identifiers.EnvelopeFrom = spfDomain
	row.Auth.SPF = DMARCAuthResult{Domain: spfDomain, Scope: scope, Result: spfResult}
	if spfResult == "" {
		row.Auth.SPF.Result = "none"
	}
	for _, d := range dkim {
		result := "fail"
		if d.Pass {
			result = "pass"
		}
		row.Auth.DKIM = append(row.Auth.DKIM, DMARCAuthResult{Domain: d.Domain, Result: result})
	}
	target, _ := json.Marshal(row)
	for i, old := range r.Records {
		old.Row.Count = 0
		b, _ := json.Marshal(old)
		if string(b) == string(target) {
			r.Records[i].Row.Count++
			return atomicJSON(path, &r)
		}
	}
	if len(r.Records) >= 10000 {
		return fmt.Errorf("DMARC report row limit reached")
	}
	row.Row.Count = 1
	r.Records = append(r.Records, row)
	return atomicJSON(path, &r)
}
func (t *DurableDMARCReports) List() ([]DMARCReport, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(t.dir, "*.json"))
	if err != nil {
		return nil, err
	}
	reports := []DMARCReport{}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r DMARCReport
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Metadata.ID < reports[j].Metadata.ID })
	return reports, nil
}
func (t *DurableDMARCReports) AuthorizedDestination(ctx context.Context, domain, uri string) (string, error) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "mailto:") {
		return "", fmt.Errorf("only mailto reporting destinations supported")
	}
	addr := strings.TrimPrefix(uri, "mailto:")
	// Size-constrained destinations are rejected rather than ignoring a limit.
	if strings.ContainsAny(addr, "!?\r\n") {
		return "", fmt.Errorf("unsupported report URI")
	}
	a, err := mail.ParseAddress(addr)
	if err != nil || a.Address != addr {
		return "", fmt.Errorf("invalid report address")
	}
	_, dest, _ := strings.Cut(addr, "@")
	if !strings.EqualFold(orgDomain(domain), orgDomain(dest)) {
		records, err := t.lookup(ctx, domain+"._report._dmarc."+dest)
		if err != nil {
			return "", err
		}
		valid := false
		for _, r := range records {
			if strings.TrimSpace(strings.SplitN(r, ";", 2)[0]) == "v=DMARC1" {
				valid = true
			}
		}
		if !valid {
			return "", fmt.Errorf("external DMARC destination not authorized")
		}
	}
	return addr, nil
}
func (t *DurableDMARCReports) SendPending(ctx context.Context) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	reports, err := t.List()
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range reports {
		if r.Metadata.Range.End >= time.Now().Unix() {
			continue
		}
		// Freeze the exact closed-day contents before sending. An observation that
		// arrived late is written to a successor report instead of being overwritten
		// by this report's destination acknowledgement.
		t.mu.Lock()
		path := filepath.Join(t.dir, r.Metadata.ID+".json")
		raw, sealErr := os.ReadFile(path)
		if sealErr == nil {
			sealErr = json.Unmarshal(raw, &r)
		}
		if sealErr == nil && !r.Sealed {
			r.Sealed = true
			sealErr = atomicJSON(path, &r)
		}
		t.mu.Unlock()
		if sealErr != nil {
			return sealErr
		}
		for _, uri := range strings.Split(r.RUA, ",") {
			if r.Sent[uri] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			address, err := t.AuthorizedDestination(ctx, r.Published.Domain, uri)
			if err != nil {
				failures = append(failures, fmt.Errorf("report %s: %w", r.Metadata.ID, err))
				continue
			}
			b, err := xml.MarshalIndent(r, "", "  ")
			if err != nil {
				return err
			}
			if t.SendMail == nil {
				return fmt.Errorf("DMARC sender unavailable")
			}
			if err = t.SendMail(ctx, address, r.Metadata.ID, append([]byte(xml.Header), b...)); err != nil {
				failures = append(failures, err)
				continue
			}
			if r.Sent == nil {
				r.Sent = map[string]bool{}
			}
			r.Sent[uri] = true
			t.mu.Lock()
			err = atomicJSON(filepath.Join(t.dir, r.Metadata.ID+".json"), &r)
			t.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return errors.Join(failures...)
}
