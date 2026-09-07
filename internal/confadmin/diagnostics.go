package confadmin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/tlsutil"
	"go.uber.org/zap/zapcore"
)

func CheckTLS(c *config.Config) error {
	if _, e := zapcore.ParseLevel(c.Logging.Level); e != nil {
		return e
	}
	pairs := []*config.TLSConfig{c.Server.TLS, c.IMAP.TLS, c.API.TLS}
	for _, l := range c.Platform.Listeners {
		pairs = append(pairs, l.TLS)
	}
	for _, p := range pairs {
		if p == nil {
			continue
		}
		if _, e := tlsutil.ServerConfig(p.Cert, p.Key, p.ClientCAFile, p.RequireClientCert); e != nil {
			return e
		}
		pair, e := tls.LoadX509KeyPair(p.Cert, p.Key)
		if e != nil {
			return e
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil {
			return e
		}
		if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return fmt.Errorf("certificate outside validity period: %s", p.Cert)
		}
	}
	return nil
}

type RelayResult struct {
	Address string `json:"address"`
	Stage   string `json:"stage"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

// RelayCheck never sends DATA. Rejected RCPT proves rejection for this probe
// only; accepted RCPT is a possible relay because DATA policy was not exercised.
func RelayCheck(ctx context.Context, address, from, to, ca string, startTLS bool) (RelayResult, error) {
	result := RelayResult{Address: address, Outcome: "inconclusive"}
	for _, v := range []string{from, to} {
		if !strings.Contains(v, "@") || strings.ContainsAny(v, "\r\n") {
			return result, fmt.Errorf("external envelope addresses required")
		}
	}
	host, _, e := net.SplitHostPort(address)
	if e != nil {
		return result, e
	}
	raw, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if e != nil {
		return result, e
	}
	defer raw.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	raw.SetDeadline(deadline)
	client, e := smtp.NewClient(raw, host)
	if e != nil {
		return result, e
	}
	defer client.Close()
	if e = client.Hello("relay-probe.invalid"); e != nil {
		return result, e
	}
	if startTLS {
		roots, e := x509.SystemCertPool()
		if e != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if ca != "" {
			b, e := os.ReadFile(ca)
			if e != nil {
				return result, e
			}
			if !roots.AppendCertsFromPEM(b) {
				return result, fmt.Errorf("invalid probe CA")
			}
		}
		if e = client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: roots}); e != nil {
			return result, e
		}
	}
	result.Stage = "MAIL"
	if e = client.Mail(from); e != nil {
		result.Detail = e.Error()
		var reply *textproto.Error
		if errors.As(e, &reply) && reply.Code >= 500 && reply.Code < 600 {
			result.Outcome = "rejected"
			return result, nil
		}
		return result, e
	}
	result.Stage = "RCPT"
	if e = client.Rcpt(to); e != nil {
		result.Detail = e.Error()
		var reply *textproto.Error
		if errors.As(e, &reply) && reply.Code >= 500 && reply.Code < 600 {
			result.Outcome = "rejected"
			return result, nil
		}
		return result, e
	}
	client.Reset()
	result.Outcome = "possible_relay"
	result.Detail = "Unauthenticated external recipient accepted; DATA was not sent"
	return result, nil
}
