package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"go.uber.org/zap"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

type Route struct {
	Domain   string    `yaml:"domain"`
	NextHops []NextHop `yaml:"next_hops"`
}
type NextHop struct {
	ClientCert  string `yaml:"client_cert"`
	ClientKey   string `yaml:"client_key"`
	reportType  string
	Address     string `yaml:"address"`
	ServerName  string `yaml:"server_name"`
	RequireTLS  bool   `yaml:"require_tls"`
	CAFile      string `yaml:"ca_file"`
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
}

func ValidateRoutes(routes []Route) error {
	seen := map[string]bool{}
	for _, r := range routes {
		d := strings.ToLower(r.Domain)
		if d == "" || strings.ContainsAny(d, " /\r\n@") || seen[d] || len(r.NextHops) == 0 {
			return fmt.Errorf("invalid or duplicate transport domain %q", r.Domain)
		}
		seen[d] = true
		for _, h := range r.NextHops {
			host, port, e := net.SplitHostPort(h.Address)
			n, portErr := strconv.Atoi(port)
			if e != nil || host == "" || portErr != nil || n < 1 || n > 65535 {
				return fmt.Errorf("invalid next hop %q", h.Address)
			}
			if (h.ClientCert != "" || h.ClientKey != "") && (!h.RequireTLS || h.ClientCert == "" || h.ClientKey == "") {
				return fmt.Errorf("connector mTLS requires TLS and both client_cert/client_key")
			}
			if (h.Username != "" || h.PasswordEnv != "") && (!h.RequireTLS || h.Username == "" || h.PasswordEnv == "") {
				return fmt.Errorf("connector authentication requires TLS, username and password_env")
			}
		}
	}
	return nil
}
func (d *MailDelivery) SetRoutes(routes []Route)    { d.routes = routes }
func (d *MailDelivery) HasRoute(domain string) bool { return len(d.route(domain)) > 0 }
func (d *MailDelivery) route(domain string) []NextHop {
	var fallback []NextHop
	for _, r := range d.routes {
		if strings.EqualFold(r.Domain, domain) {
			return r.NextHops
		}
		if r.Domain == "*" {
			fallback = r.NextHops
		}
	}
	return fallback
}
func (d *MailDelivery) deliverRoute(ctx context.Context, hops []NextHop, from string, to []string, data []byte) (*DeliveryResult, error) {
	var last error
	for _, h := range hops {
		c, err := d.dialNextHop(ctx, h)
		if d.reports != nil && h.RequireTLS && len(to) > 0 {
			parts := strings.Split(to[0], "@")
			if len(parts) == 2 {
				kind := h.reportType
				if kind == "" {
					kind = "no-policy-found"
				}
				if e := d.reports.Record(parts[1], h.Address, kind, err == nil); e != nil {
					d.logger.Error("TLS report persistence failed", zap.Error(e))
				}
			}
		}
		if err != nil {
			last = err
			continue
		}
		result, err := smtpTransaction(c, h.Address, from, to, data)
		c.Close()
		if err == nil || result.IsPermanent {
			return result, err
		}
		last = err
	}
	return &DeliveryResult{SMTPCode: 451, Message: "All configured next hops unavailable"}, fmt.Errorf("next hop delivery: %w", last)
}
func (d *MailDelivery) dialNextHop(ctx context.Context, h NextHop) (*smtp.Client, error) {
	raw, err := (&net.Dialer{Timeout: d.connectTimeout}).DialContext(ctx, "tcp", h.Address)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(d.dataTimeout)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	raw.SetDeadline(deadline)
	host, _, _ := net.SplitHostPort(h.Address)
	c, err := smtp.NewClient(raw, host)
	if err != nil {
		raw.Close()
		return nil, err
	}
	fail := func(err error) (*smtp.Client, error) { c.Close(); return nil, err }
	if err = c.Hello(d.hostname); err != nil {
		return fail(err)
	}
	if h.RequireTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fail(fmt.Errorf("connector requires STARTTLS"))
		}
		name := h.ServerName
		if name == "" {
			name = host
		}
		tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name}
		if h.ClientCert != "" {
			pair, err := tls.LoadX509KeyPair(h.ClientCert, h.ClientKey)
			if err != nil {
				return fail(err)
			}
			tc.Certificates = []tls.Certificate{pair}
		}
		if h.CAFile != "" {
			pem, e := os.ReadFile(h.CAFile)
			if e != nil {
				return fail(e)
			}
			pool, _ := x509.SystemCertPool()
			if pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return fail(fmt.Errorf("invalid connector CA"))
			}
			tc.RootCAs = pool
		}
		if err = c.StartTLS(tc); err != nil {
			return fail(err)
		}
	}
	if h.Username != "" {
		secret := os.Getenv(h.PasswordEnv)
		if secret == "" {
			return fail(fmt.Errorf("missing connector secret environment variable"))
		}
		if err = c.Auth(smtp.PlainAuth("", h.Username, secret, host)); err != nil {
			return fail(err)
		}
	}
	return c, nil
}

// smtpTransaction marks recipients successful only after DATA is acknowledged.
func smtpTransaction(c *smtp.Client, host, from string, recipients []string, data []byte) (*DeliveryResult, error) {
	r := &DeliveryResult{RemoteHost: host}
	fail := func(err error) (*DeliveryResult, error) {
		code, permanent := parseSMTPError(err)
		if code == 0 {
			code = 451
		}
		r.SMTPCode = code
		r.IsPermanent = permanent
		r.Message = err.Error()
		for i := range r.Recipients {
			if r.Recipients[i].SMTPCode == 0 {
				r.Recipients[i].SMTPCode = code
				r.Recipients[i].IsPermanent = permanent
				r.Recipients[i].Message = err.Error()
			}
		}
		return r, err
	}
	if err := c.Mail(from); err != nil {
		return fail(err)
	}
	accepted := 0
	for _, to := range recipients {
		outcome := RecipientResult{Recipient: to}
		if err := c.Rcpt(to); err != nil {
			outcome.SMTPCode, outcome.IsPermanent = parseSMTPError(err)
			if outcome.SMTPCode == 0 {
				outcome.SMTPCode = 451
			}
			outcome.Message = err.Error()
		} else {
			accepted++
		}
		r.Recipients = append(r.Recipients, outcome)
	}
	if accepted == 0 {
		r.IsPermanent = true
		r.SMTPCode = 550
		r.Message = "No recipients accepted"
		for _, v := range r.Recipients {
			if !v.IsPermanent {
				r.IsPermanent = false
				r.SMTPCode = 451
			}
		}
		return r, fmt.Errorf("no recipients accepted")
	}
	w, err := c.Data()
	if err != nil {
		return fail(err)
	}
	if _, err = w.Write(data); err != nil {
		c.Close()
		return fail(err)
	}
	if err = w.Close(); err != nil {
		return fail(err)
	}
	r.Success = true
	r.SMTPCode = 250
	r.Message = "Message accepted"
	r.DeliveredAt = time.Now()
	for i := range r.Recipients {
		if r.Recipients[i].SMTPCode == 0 {
			r.Recipients[i].Success = true
			r.Recipients[i].SMTPCode = 250
		}
	}
	return r, nil
}

func (d *MailDelivery) HasExplicitRoute(domain string) bool {
	for _, r := range d.routes {
		if r.Domain != "*" && strings.EqualFold(r.Domain, domain) {
			return true
		}
	}
	return false
}
