package bounce

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

type Config struct {
	TrackIncoming          bool          `yaml:"track_incoming" json:"track_incoming"`
	EnableDSN              bool          `yaml:"enable_dsn" json:"enable_dsn"`
	DelayWarningAfter      time.Duration `yaml:"delay_warning_after" json:"delay_warning_after"`
	FullReturnMaxBytes     int           `yaml:"full_return_max_bytes" json:"full_return_max_bytes"`
	SuppressedRecipients   []string      `yaml:"suppressed_recipients" json:"suppressed_recipients"`
	Suppress               bool          `yaml:"suppress" json:"suppress"`
	Postmaster             string        `yaml:"postmaster" json:"postmaster"`
	IncludeOriginalHeaders *bool         `yaml:"include_original_headers" json:"include_original_headers"`
	MaxHeaderBytes         int           `yaml:"max_header_bytes" json:"max_header_bytes"`
	MaxAttempts            int           `yaml:"max_attempts" json:"max_attempts"`
	InitialDelay           time.Duration `yaml:"initial_delay" json:"initial_delay"`
	MaxDelay               time.Duration `yaml:"max_delay" json:"max_delay"`
}

func (c Config) Defaults() Config {
	if c.MaxHeaderBytes == 0 {
		c.MaxHeaderBytes = 1024
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 5
	}
	if c.InitialDelay == 0 {
		c.InitialDelay = time.Minute
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = 4 * time.Hour
	}
	return c
}
func (c Config) Validate() error {
	if c.DelayWarningAfter < 0 || c.FullReturnMaxBytes < 0 || c.FullReturnMaxBytes > 25<<20 {
		return fmt.Errorf("invalid DSN limits")
	}
	for _, address := range c.SuppressedRecipients {
		a, e := mail.ParseAddress(address)
		if e != nil || a.Address != address {
			return fmt.Errorf("invalid suppressed recipient")
		}
	}
	c = c.Defaults()
	if c.MaxHeaderBytes < 1 || c.MaxHeaderBytes > 65536 || c.MaxAttempts < 1 || c.MaxAttempts > 100 || c.InitialDelay < time.Second || c.MaxDelay < c.InitialDelay || c.MaxDelay > 7*24*time.Hour {
		return fmt.Errorf("invalid bounce limits")
	}
	if c.Postmaster != "" {
		a, e := mail.ParseAddress(c.Postmaster)
		if e != nil || a.Address != c.Postmaster || strings.ContainsAny(c.Postmaster, "\r\n") {
			return fmt.Errorf("invalid bounce postmaster")
		}
	}
	return nil
}
func (bg *BounceGenerator) Configure(c Config) {
	bg.config = c.Defaults()
	if c.Postmaster != "" {
		bg.postmaster = c.Postmaster
	}
}
func cleanField(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
