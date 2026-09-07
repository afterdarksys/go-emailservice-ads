package main

import (
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/jmap"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"github.com/afterdarksys/go-emailservice-ads/internal/tlsutil"
	"go.uber.org/zap"
)

// Preflight is read-only. Runtime availability (ports/providers/disks) is still
// checked by startup after old resources are released.
func validateReloadConfig(path string, current ...*config.Config) error {
	if err := validateConfigFile(path); err != nil {
		return err
	}
	c, err := config.LoadConfig(path)
	if err != nil {
		return err
	}
	if len(current) > 0 {
		if err := c.ValidateReloadFrom(current[0]); err != nil {
			return err
		}
	}
	configs := []*config.TLSConfig{c.Server.TLS, c.IMAP.TLS, c.API.TLS}
	for _, l := range c.Platform.Listeners {
		configs = append(configs, l.TLS)
	}
	for _, tc := range configs {
		if tc == nil {
			continue
		}
		if _, err = tlsutil.ServerConfig(tc.Cert, tc.Key, tc.ClientCAFile, tc.RequireClientCert); err != nil {
			return fmt.Errorf("reload TLS: %w", err)
		}
	}
	// Constructing the LDAP provider validates its filter, CA and bind-secret file
	// without contacting the directory.
	if c.Auth.LDAP.Enabled {
		if _, err = auth.NewLDAPProvider(c.Auth.LDAP); err != nil {
			return err
		}
	}
	if c.JMAP.JWTPublicKeyPath != "" {
		if err := jmap.ValidatePublicKey(c.JMAP.JWTPublicKeyPath); err != nil {
			return err
		}
	}
	for _, key := range c.Server.DKIM {
		if _, err := security.NewSigner(zap.NewNop(), key.Domain, key.Selector, key.PrivateKeyPath); err != nil {
			return err
		}
	}
	return nil
}
