// mailflow-probe is a synthetic end-to-end monitor for the public mail path.
// It sends a uniquely tagged message through SMTP, waits until IMAP can see it,
// removes that exact message, and exposes its results for Prometheus.
package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type probeConfig struct {
	smtpAddr, imapAddr, sender, recipient, imapUser, imapPassword string
	interval, timeout                                             time.Duration
}

type probeMetrics struct {
	mu          sync.RWMutex
	runs        uint64
	failures    uint64
	lastSuccess bool
	lastLatency float64
	lastRun     time.Time
}

func main() {
	cfg := probeConfig{}
	flag.StringVar(&cfg.smtpAddr, "smtp-addr", env("MAILFLOW_SMTP_ADDR", ""), "public SMTP address, normally mail.gomeow.media:25")
	flag.StringVar(&cfg.imapAddr, "imap-addr", env("MAILFLOW_IMAP_ADDR", ""), "IMAPS address, normally mail.gomeow.media:993")
	flag.StringVar(&cfg.sender, "sender", env("MAILFLOW_SENDER", "mailflow-probe@gomeow.media"), "envelope sender")
	flag.StringVar(&cfg.recipient, "recipient", env("MAILFLOW_RECIPIENT", "mailflow-probe@gomeow.media"), "local probe mailbox recipient")
	flag.StringVar(&cfg.imapUser, "imap-user", env("MAILFLOW_IMAP_USER", ""), "IMAP username")
	flag.StringVar(&cfg.imapPassword, "imap-password", env("MAILFLOW_IMAP_PASSWORD", ""), "IMAP password (prefer MAILFLOW_IMAP_PASSWORD environment variable)")
	flag.DurationVar(&cfg.interval, "interval", durationEnv("MAILFLOW_INTERVAL", 5*time.Minute), "interval between probes")
	flag.DurationVar(&cfg.timeout, "timeout", durationEnv("MAILFLOW_TIMEOUT", time.Minute), "maximum end-to-end probe time")
	listen := flag.String("listen", env("MAILFLOW_LISTEN", ":9765"), "metrics and health listener")
	flag.Parse()

	if cfg.smtpAddr == "" || cfg.imapAddr == "" || cfg.imapUser == "" || cfg.imapPassword == "" {
		log.Fatal("smtp-addr, imap-addr, imap-user, and imap-password are required")
	}
	metrics := &probeMetrics{}
	http.HandleFunc("/healthz", metrics.health)
	http.HandleFunc("/metrics", metrics.prometheus)
	go func() { log.Fatal(http.ListenAndServe(*listen, nil)) }()

	run := func() { metrics.record(runProbe(cfg)) }
	run()
	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()
	for range ticker.C {
		run()
	}
}

func runProbe(cfg probeConfig) (time.Duration, error) {
	started := time.Now()
	id, err := randomID()
	if err != nil {
		return 0, fmt.Errorf("create probe id: %w", err)
	}
	if err := sendSMTP(cfg, id); err != nil {
		return 0, fmt.Errorf("smtp: %w", err)
	}
	deadline := started.Add(cfg.timeout)
	for time.Now().Before(deadline) {
		found, err := findAndDeleteIMAP(cfg, id)
		if err != nil {
			return 0, fmt.Errorf("imap: %w", err)
		}
		if found {
			return time.Since(started), nil
		}
		time.Sleep(2 * time.Second)
	}
	return 0, fmt.Errorf("message did not reach IMAP within %s", cfg.timeout)
}

func sendSMTP(cfg probeConfig, id string) error {
	c, err := smtp.Dial(cfg.smtpAddr)
	if err != nil {
		return err
	}
	defer c.Quit()
	if err := c.Hello("mailflow-probe.gomeow.media"); err != nil {
		return err
	}
	if err := c.Mail(cfg.sender); err != nil {
		return err
	}
	if err := c.Rcpt(cfg.recipient); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "From: "+cfg.sender+"\r\nTo: "+cfg.recipient+"\r\nSubject: Mail flow probe\r\nX-Mailflow-Probe-ID: "+id+"\r\nAuto-Submitted: auto-generated\r\n\r\nSynthetic monitor message. Safe to delete.\r\n")
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	return err
}

func findAndDeleteIMAP(cfg probeConfig, id string) (bool, error) {
	c, err := client.DialTLS(cfg.imapAddr, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host(cfg.imapAddr)})
	if err != nil {
		return false, err
	}
	defer c.Logout()
	if err := c.Login(cfg.imapUser, cfg.imapPassword); err != nil {
		return false, err
	}
	if _, err := c.Select("INBOX", false); err != nil {
		return false, err
	}
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("X-Mailflow-Probe-ID", id)
	uids, err := c.UidSearch(criteria)
	if err != nil || len(uids) == 0 {
		return false, err
	}
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	if err := c.UidStore(set, imap.FormatFlagsOp(imap.AddFlags, true), []string{imap.DeletedFlag}, nil); err != nil {
		return false, err
	}
	if err := c.Expunge(nil); err != nil {
		return false, err
	}
	return true, nil
}

func (m *probeMetrics) record(latency time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs++
	m.lastRun = time.Now()
	m.lastSuccess = err == nil
	if err != nil {
		m.failures++
		log.Printf("mail flow probe failed: %v", err)
		return
	}
	m.lastLatency = latency.Seconds()
	log.Printf("mail flow probe succeeded in %s", latency.Round(time.Millisecond))
}

func (m *probeMetrics) health(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.lastSuccess {
		http.Error(w, "latest mail flow probe failed", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (m *probeMetrics) prometheus(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := 0
	if m.lastSuccess {
		status = 1
	}
	fmt.Fprintf(w, "mailflow_probe_runs_total %d\nmailflow_probe_failures_total %d\nmailflow_probe_last_success %d\nmailflow_probe_latency_seconds %.6f\nmailflow_probe_last_run_timestamp_seconds %d\n", m.runs, m.failures, status, m.lastLatency, m.lastRun.Unix())
}

func randomID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func durationEnv(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v != 0 {
		return v
	}
	return fallback
}
func host(address string) string {
	h, _, err := strings.Cut(address, ":")
	if err {
		return h
	}
	return address
}
