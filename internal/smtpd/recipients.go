package smtpd

import (
	"context"
	"encoding/json"
	"github.com/emersion/go-smtp"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func (s *Session) resolveRecipient(address string, seen map[string]bool) ([]string, error) {
	key := strings.ToLower(address)
	if seen[key] || len(seen) > 20 {
		return nil, &smtp.SMTPError{Code: 554, Message: "Alias routing loop"}
	}
	seen[key] = true
	defer delete(seen, key)
	if targets, ok := s.config.Platform.Aliases[key]; ok {
		var resolved []string
		for _, t := range targets {
			v, e := s.resolveRecipient(t, seen)
			if e != nil {
				return nil, e
			}
			resolved = append(resolved, v...)
		}
		return resolved, nil
	}
	local := false
	for _, d := range s.config.Server.LocalDomains {
		if strings.EqualFold(d, addressDomain(address)) {
			local = true
			break
		}
	}
	if local && s.config.Platform.ValidateRecipients {
		if endpoint := s.config.Platform.RecipientDirectoryURL; endpoint != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(endpoint, "/")+"/"+url.PathEscape(address), nil)
			if err != nil {
				return nil, &smtp.SMTPError{Code: 451, Message: "Invalid recipient directory configuration"}
			}
			req.Header.Set("Authorization", "Bearer "+os.Getenv(s.config.Platform.RecipientDirectoryTokenEnv))
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				return nil, &smtp.SMTPError{Code: 451, Message: "Recipient directory unavailable"}
			}
			defer resp.Body.Close()
			if resp.StatusCode == 404 {
				return nil, &smtp.SMTPError{Code: 550, Message: "Unknown or disabled recipient"}
			}
			if resp.StatusCode != 200 {
				return nil, &smtp.SMTPError{Code: 451, Message: "Recipient directory unavailable"}
			}
			var result struct {
				Recipients []string `json:"recipients"`
			}
			if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result); err != nil || len(result.Recipients) == 0 || len(result.Recipients) > 1000 {
				return nil, &smtp.SMTPError{Code: 451, Message: "Invalid recipient directory response"}
			}
			for _, target := range result.Recipients {
				if addressDomain(target) == "" || strings.ContainsAny(target, "\r\n") {
					return nil, &smtp.SMTPError{Code: 451, Message: "Invalid directory recipient"}
				}
			}
			return result.Recipients, nil
		}

		if s.validator == nil {
			return nil, &smtp.SMTPError{Code: 451, Message: "Recipient directory unavailable"}
		}
		u, ok := s.validator.GetUserStore().Recipient(address)
		if !ok {
			return nil, &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "Unknown or disabled recipient"}
		}
		return []string{u.Email}, nil
	}
	return []string{address}, nil
}
