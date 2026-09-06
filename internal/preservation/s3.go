// Package preservation archives artifacts using S3 Object Lock COMPLIANCE mode.
package preservation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	MaxBytes       int64         `yaml:"max_bytes"`
	SourceFile     string        `yaml:"source_file"`
	Bucket         string        `yaml:"bucket"`
	Prefix         string        `yaml:"prefix"`
	Region         string        `yaml:"region"`
	Endpoint       string        `yaml:"endpoint"`
	Profile        string        `yaml:"profile"`
	RetainUntil    time.Time     `yaml:"retain_until"`
	LegalHold      bool          `yaml:"legal_hold"`
	AllowLocalHTTP bool          `yaml:"allow_local_http"`
	Timeout        time.Duration `yaml:"timeout"`
}
type Receipt struct {
	Bucket      string    `json:"bucket"`
	Key         string    `json:"key"`
	VersionID   string    `json:"version_id"`
	SHA256      string    `json:"sha256"`
	RetainUntil time.Time `json:"retain_until"`
	LegalHold   bool      `json:"legal_hold"`
}
type Runner func(context.Context, []string) ([]byte, error)

func AWS(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	cmd.Env = append(os.Environ(), "AWS_PAGER=")
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 252 {
			return nil, fmt.Errorf("AWS CLI argument validation: %.2048s", exit.Stderr)
		}
		return nil, fmt.Errorf("AWS CLI request failed: %w", err)
	}
	return output, nil
}
func (c Config) Validate() error {
	if c.MaxBytes < 0 || c.MaxBytes > 5<<30 {
		return fmt.Errorf("max_bytes must be 0..5 GiB; larger artifacts require splitting")
	}
	if c.SourceFile == "" || c.Bucket == "" || !c.RetainUntil.After(time.Now()) || c.Timeout < 0 {
		return fmt.Errorf("source_file, bucket and future retain_until are required")
	}
	if c.Endpoint != "" {
		u, e := url.Parse(c.Endpoint)
		if e != nil {
			return e
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("preservation endpoint must not contain credentials, query or fragment")
		}
		local := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
		if u.Scheme != "https" && !(c.AllowLocalHTTP && local && u.Scheme == "http") {
			return fmt.Errorf("preservation endpoint must use HTTPS")
		}
	}
	return nil
}
func Archive(ctx context.Context, c Config, run Runner) (*Receipt, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Upload a private immutable snapshot so source changes cannot invalidate the checksum.
	in, err := os.Open(c.SourceFile)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("regular artifact required")
	}
	temp, err := os.CreateTemp("", "mailhub-preserve-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	hash := sha256.New()
	limit := c.MaxBytes
	if limit == 0 {
		limit = 1 << 30
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("artifact exceeds max_bytes")
	}
	n, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(in, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, fmt.Errorf("artifact grew beyond max_bytes")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err = temp.Sync(); err != nil {
		return nil, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	key := strings.Trim(c.Prefix, "/") + "/" + digest + "/" + filepath.Base(c.SourceFile)
	key = strings.TrimLeft(key, "/")
	base := []string{"--output", "json"}
	if c.Endpoint != "" {
		base = append(base, "--endpoint-url", c.Endpoint)
	}
	if c.Region != "" {
		base = append(base, "--region", c.Region)
	}
	if c.Profile != "" {
		base = append(base, "--profile", c.Profile)
	}
	call := func(args ...string) ([]byte, error) { return run(ctx, append(append([]string{}, base...), args...)) }
	raw, err := call("s3api", "get-object-lock-configuration", "--bucket", c.Bucket)
	if err != nil {
		return nil, err
	}
	var lock struct {
		Configuration struct {
			Enabled string `json:"ObjectLockEnabled"`
		} `json:"ObjectLockConfiguration"`
	}
	if json.Unmarshal(raw, &lock) != nil || lock.Configuration.Enabled != "Enabled" {
		return nil, fmt.Errorf("bucket Object Lock is not enabled")
	}
	hold := "OFF"
	if c.LegalHold {
		hold = "ON"
	}
	raw, err = call("s3api", "put-object", "--bucket", c.Bucket, "--key", key, "--body", temp.Name(), "--metadata", "sha256="+digest, "--object-lock-mode", "COMPLIANCE", "--object-lock-retain-until-date", c.RetainUntil.UTC().Format(time.RFC3339), "--object-lock-legal-hold-status", hold)
	if err != nil {
		return nil, err
	}
	var put struct {
		VersionID string `json:"VersionId"`
	}
	if json.Unmarshal(raw, &put) != nil || put.VersionID == "" {
		return nil, fmt.Errorf("archive has no version ID")
	}
	raw, err = call("s3api", "head-object", "--bucket", c.Bucket, "--key", key, "--version-id", put.VersionID)
	if err != nil {
		return nil, err
	}
	var head struct {
		Mode     string            `json:"ObjectLockMode"`
		Until    time.Time         `json:"ObjectLockRetainUntilDate"`
		Hold     string            `json:"ObjectLockLegalHoldStatus"`
		Metadata map[string]string `json:"Metadata"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Mode != "COMPLIANCE" || head.Until.Before(c.RetainUntil.Truncate(time.Second)) || head.Hold != hold || head.Metadata["sha256"] != digest {
		return nil, fmt.Errorf("archive retention verification failed")
	}
	return &Receipt{Bucket: c.Bucket, Key: key, VersionID: put.VersionID, SHA256: digest, RetainUntil: head.Until, LegalHold: c.LegalHold}, nil
}
