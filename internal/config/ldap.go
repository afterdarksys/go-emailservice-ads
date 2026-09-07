package config

import (
	"fmt"
	"net/url"
	"strings"
)

type LDAPConfig struct {
	Enabled      bool     `yaml:"enabled"`
	URL          string   `yaml:"url"`
	BaseDN       string   `yaml:"base_dn"`
	BindDN       string   `yaml:"bind_dn"`
	PasswordFile string   `yaml:"password_file"`
	CAFile       string   `yaml:"ca_file"`
	UserFilter   string   `yaml:"user_filter"`
	Domains      []string `yaml:"domains"`
}

func (c LDAPConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "ldaps" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("auth.ldap.url must be an ldaps:// server URL")
	}
	if c.BaseDN == "" || c.BindDN == "" || c.PasswordFile == "" || strings.Count(c.UserFilter, "{username}") != 1 || len(c.Domains) == 0 {
		return fmt.Errorf("auth.ldap requires base_dn, bind_dn, password_file, one {username} in user_filter, and domains")
	}
	for _, d := range c.Domains {
		if d == "" || strings.ContainsAny(d, "@ /\\\r\n") {
			return fmt.Errorf("invalid auth.ldap domain")
		}
	}
	return nil
}
