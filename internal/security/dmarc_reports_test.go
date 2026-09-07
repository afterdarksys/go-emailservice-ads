package security

import (
	"context"
	"encoding/xml"
	"testing"
	"time"
)

func TestDMARCAggregationRestartAndExternalAuthorization(t *testing.T) {
	dir := t.TempDir()
	r, err := NewDurableDMARCReports(dir, "receiver.test")
	if err != nil {
		t.Fatal(err)
	}
	e := DMARCEvaluation{Published: DMARCPolicySnapshot{Domain: "sender.example", ADKIM: "r", ASPF: "r", Policy: DMARCPolicyReject, SubPolicy: DMARCPolicyReject, Pct: 100}, RUA: "mailto:reports@collector.test", SPFAligned: true}
	at := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 2; i++ {
		if err = r.Record(at, e, "192.0.2.1", "sender.example", "sender.example", "pass", "mfrom", "none", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	r, err = NewDurableDMARCReports(dir, "receiver.test")
	if err != nil {
		t.Fatal(err)
	}
	reports, err := r.List()
	if err != nil || len(reports) != 1 || reports[0].Records[0].Row.Count != 2 {
		t.Fatal(reports, err)
	}
	sent := 0
	r.SendMail = func(_ context.Context, to, id string, raw []byte) error {
		sent++
		var decoded DMARCReport
		if err := xml.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		if decoded.Version != "1.0" || decoded.Records[0].Row.Evaluated.SPF != "pass" {
			t.Fatal(decoded)
		}
		return nil
	}
	r.lookup = func(context.Context, string) ([]string, error) { return []string{}, nil }
	if err = r.SendPending(context.Background()); err == nil || sent != 0 {
		t.Fatal("unauthorized external destination")
	}
	r.lookup = func(_ context.Context, name string) ([]string, error) {
		if name != "sender.example._report._dmarc.collector.test" {
			t.Fatal(name)
		}
		return []string{"v=DMARC1"}, nil
	}
	if err = r.SendPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.SendPending(context.Background()); err != nil || sent != 1 {
		t.Fatal(sent, err)
	}
}

func TestDMARCLateObservationAndIndependentDestinations(t *testing.T) {
	r, err := NewDurableDMARCReports(t.TempDir(), "receiver.test")
	if err != nil {
		t.Fatal(err)
	}
	e := DMARCEvaluation{Published: DMARCPolicySnapshot{Domain: "sender.example"}, RUA: "mailto:bad@external.test,mailto:good@sender.example"}
	at := time.Now().Add(-48 * time.Hour)
	record := func() {
		if err := r.Record(at, e, "192.0.2.1", "sender.example", "sender.example", "pass", "mfrom", "none", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	record()
	r.lookup = func(context.Context, string) ([]string, error) { return nil, nil }
	calls := 0
	r.SendMail = func(_ context.Context, to, id string, raw []byte) error {
		calls++
		if to != "good@sender.example" {
			t.Fatal(to)
		}
		if calls == 1 {
			record()
		}
		return nil
	}
	if err = r.SendPending(context.Background()); err == nil {
		t.Fatal("missing external authorization failure")
	}
	if calls != 1 {
		t.Fatal("one destination prevented another", calls)
	}
	reports, err := r.List()
	if err != nil || len(reports) != 2 {
		t.Fatal(reports, err)
	}
	count := int64(0)
	for _, v := range reports {
		for _, row := range v.Records {
			count += row.Row.Count
		}
	}
	if count != 2 {
		t.Fatal("late event lost", count)
	}
}
