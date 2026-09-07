package auth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/go-ldap/ldap/v3"
)

type directoryAuthenticator interface {
	Matches(string) bool
	Authenticate(string, string) error
}

type LDAPProvider struct {
	config config.LDAPConfig
	tls    *tls.Config
}

func NewLDAPProvider(c config.LDAPConfig) (*LDAPProvider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.CAFile != "" {
		b, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("LDAP CA file contains no certificates")
		}
		tc.RootCAs = roots
	}
	if _, err := ldap.CompileFilter(strings.Replace(c.UserFilter, "{username}", "validation", 1)); err != nil {
		return nil, fmt.Errorf("LDAP filter: %w", err)
	}
	if _, err := readLDAPPassword(c.PasswordFile); err != nil {
		return nil, err
	}
	return &LDAPProvider{config: c, tls: tc}, nil
}
func readLDAPPassword(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(b))
	if len(value) == 0 || len(b) > 4096 {
		return "", fmt.Errorf("LDAP bind password must be nonempty and at most 4096 bytes")
	}
	return value, nil
}
func (p *LDAPProvider) Matches(user string) bool {
	_, domain, ok := strings.Cut(user, "@")
	if !ok {
		return false
	}
	for _, d := range p.config.Domains {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}
func (p *LDAPProvider) Authenticate(user, password string) error {
	if password == "" || len(password) > 4096 {
		return ErrInvalidCredentials
	}
	secret, err := readLDAPPassword(p.config.PasswordFile)
	if err != nil {
		return err
	}
	conn, err := ldap.DialURL(p.config.URL, ldap.DialWithTLSConfig(p.tls.Clone()), ldap.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetTimeout(5 * time.Second)
	if err = conn.Bind(p.config.BindDN, secret); err != nil {
		return err
	}
	filter := strings.Replace(p.config.UserFilter, "{username}", ldap.EscapeFilter(user), 1)
	result, err := conn.Search(ldap.NewSearchRequest(p.config.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 5, false, filter, []string{"dn"}, nil))
	if err != nil {
		return err
	}
	if len(result.Entries) != 1 {
		return ErrInvalidCredentials
	}
	return conn.Bind(result.Entries[0].DN, password)
}

// SetLDAPProvider is called at startup before accepting connections.
func (s *UserStore) SetLDAPProvider(p *LDAPProvider) {
	if p != nil {
		s.directory = p
	}
}
