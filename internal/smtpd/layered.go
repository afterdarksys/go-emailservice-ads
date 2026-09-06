package smtpd

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/afterdarksys/go-emailservice-ads/internal/config"
	"github.com/afterdarksys/go-emailservice-ads/internal/filtering"
	"github.com/afterdarksys/go-emailservice-ads/internal/ipfilter"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/security"
	"github.com/emersion/go-smtp"
)

// layerChecks strips untrusted control headers before they reach another hub.
func (s *Session) layerChecks() error {
	parsed, err := mail.ReadMessage(bytes.NewReader(s.msg.Data))
	if err != nil {
		return &smtp.SMTPError{Code: 550, Message: "Malformed message headers"}
	}
	if s.authenticated {
		from := headerFromDomain(s.msg.Data)
		visible, err := mail.ParseAddress(parsed.Header.Get("From"))
		if from == "" || err != nil || !s.validator.AuthorizedToSendAs(s.username, visible.Address) {
			return &smtp.SMTPError{Code: 550, Message: "Not authorized for message From identity"}
		}
	}
	max := s.config.Platform.MaxHops
	if max == 0 {
		max = 30
	}
	if len(parsed.Header["Received"]) >= max {
		return &smtp.SMTPError{Code: 554, Message: "Mail routing hop limit exceeded"}
	}
	if s.trustedFilter {
		origin := parsed.Header.Get("X-Mailhub-Original-IP")
		if origin != "" {
			if net.ParseIP(origin) == nil {
				return &smtp.SMTPError{Code: 550, Message: "Invalid trusted origin metadata"}
			}
			s.msg.ClientIP = origin
			if code, reason := ipfilter.Check(context.Background(), s.config.Server.IPFilter, origin, nil); code != 0 {
				return &smtp.SMTPError{Code: code, Message: reason}
			}
			if s.policyEngine != nil && s.config.Server.SPF.Enabled {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				domain, identity := spfIdentityForMailFrom(s.msg.From, s.ehlo)
				result, e := s.policyEngine.VerifySPF(ctx, net.ParseIP(origin), domain, identity)
				cancel()
				if e == nil {
					s.msg.SPFResult = string(result)
					if strings.EqualFold(s.config.Server.SPF.Mode, "enforce") && (result == security.SPFFail || result == security.SPFSoftFail && s.config.Server.SPF.RejectOnSoftfail) {
						return &smtp.SMTPError{Code: 550, Message: "SPF validation failed"}
					}
				} else {
					s.msg.SPFResult = string(security.SPFTempError)
				}
			}
		}
	}
	s.msg.Data = removeHeader(s.msg.Data, "X-Mailhub-Original-IP")
	// Recompute local verdicts; forged Authentication-Results cannot survive as
	// a claim by this installation. ARC headers remain intact for verification.
	s.msg.Data = removeHeader(s.msg.Data, "Authentication-Results")
	s.reputation = policy.ReputationScore{Score: 50, Source: "unknown"}
	if s.config.Platform.ReputationURL != "" || s.qManager != nil && s.qManager.reputationDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var r filtering.Reputation
		var e error
		if s.qManager != nil && s.qManager.reputationDB != nil {
			r, e = s.qManager.reputationDB.Lookup(ctx, s.msg.ClientIP)
		} else {
			r, e = filtering.LookupReputation(ctx, s.config.Platform.ReputationURL, s.msg.ClientIP)
		}
		cancel()
		if e != nil {
			if s.config.Platform.ReputationRequired {
				return &smtp.SMTPError{Code: 451, Message: "Required reputation service unavailable"}
			}
		} else if r.Known {
			s.reputation = policy.ReputationScore{Score: r.Score, Source: r.Source}
			if r.Score < s.config.Platform.ReputationRejectBelow {
				return &smtp.SMTPError{Code: 550, Message: "Sender IP reputation below required threshold"}
			}
		}
	} else if s.config.Platform.ReputationRequired {
		return &smtp.SMTPError{Code: 451, Message: "Required reputation service unavailable"}
	}
	s.msg.Data = append([]byte(fmt.Sprintf("Received: from %s ([%s]) by %s; %s\r\n", sanitizeHeaderValue(s.ehlo), sanitizeHeaderValue(s.msg.ClientIP), sanitizeHeaderValue(s.config.Server.Domain), time.Now().Format(time.RFC1123Z))), s.msg.Data...)
	return nil
}
func applyPolicyHeaders(raw []byte, headers []policy.Header) ([]byte, error) {
	for _, h := range headers {
		if h.Name == "" || strings.ContainsAny(h.Name, ":\r\n \t") || strings.ContainsAny(h.Value, "\r\n") {
			return nil, fmt.Errorf("invalid policy header")
		}
		if h.Action == "remove" || h.Action == "replace" {
			raw = removeHeader(raw, h.Name)
		}
		if h.Action == "add" || h.Action == "replace" {
			raw = append([]byte(h.Name+": "+h.Value+"\r\n"), raw...)
		}
	}
	return raw, nil
}

func (s *Session) scanFinal() error {
	previousARC := countHeader(s.msg.Data, "Arc-Seal")
	if err := scanMalware(context.Background(), s.config.Platform, s.msg.Data); err != nil {
		return err
	}

	if s.config.Platform.ScannerURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), configuredDuration(s.config.Platform.ScannerTimeout, 15*time.Second))
		verdict, replacement, e := filtering.ScanFinal(ctx, s.config.Platform.ScannerURL, s.msg.From, s.msg.ClientIP, s.ehlo, s.username, s.msg.To, s.msg.Data, s.config.Platform.ARC)
		cancel()
		if e != nil {
			if s.config.Platform.ScannerRequired {
				return &smtp.SMTPError{Code: 451, Message: "Required content scanner unavailable"}
			}
		} else {
			if replacement != nil {
				s.msg.Data = replacement
			}
			switch verdict.Action {
			case "reject":
				return &smtp.SMTPError{Code: 550, Message: "Message rejected by content policy"}
			case "soft reject", "greylist":
				return &smtp.SMTPError{Code: 451, Message: "Content policy temporarily deferred message"}
			case "quarantine", "discard":
				s.msg.Quarantine = true
				s.msg.QuarantineFolder = "Scanner: " + verdict.Action
			case "add header":
				if replacement == nil && !s.config.Platform.ARC {
					s.msg.Data = append([]byte("X-Spam: Yes\r\n"), s.msg.Data...)
				}
			case "rewrite subject":
				if s.config.Platform.ARC && replacement == nil {
					return &smtp.SMTPError{Code: 451, Message: "ARC signing requires scanner-rewritten subject"}
				}
				if replacement == nil {
					s.msg.Data = removeHeader(s.msg.Data, "Subject")
					s.msg.Data = append([]byte("Subject: "+sanitizeHeaderValue(verdict.Subject)+"\r\n"), s.msg.Data...)
				}
			}
			if replacement == nil {
				modified, err := filtering.ApplyHeaders(s.msg.Data, verdict)
				if err != nil {
					return &smtp.SMTPError{Code: 451, Message: "Invalid scanner header operations"}
				}
				s.msg.Data = modified
			}
		}
	} else if s.config.Platform.ScannerRequired {
		return &smtp.SMTPError{Code: 451, Message: "Required content scanner unavailable"}
	}

	if s.config.Platform.ARC && !s.msg.Quarantine {
		count := countHeader(s.msg.Data, "Arc-Seal")
		if count <= previousARC || countHeader(s.msg.Data, "Arc-Message-Signature") != count || countHeader(s.msg.Data, "Arc-Authentication-Results") != count {
			return &smtp.SMTPError{Code: 451, Message: "Required ARC sealing unavailable"}
		}
	}

	return nil
}

func scanMalware(parent context.Context, p config.PlatformConfig, data []byte) error {
	if p.ClamAVAddress == "" {
		if p.MalwareRequired {
			return &smtp.SMTPError{Code: 451, Message: "Required malware scanner unavailable"}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, configuredDuration(p.ScannerTimeout, 15*time.Second))
	defer cancel()
	found, err := filtering.ScanClamAV(ctx, p.ClamAVAddress, data)
	if err != nil {
		if p.MalwareRequired {
			return &smtp.SMTPError{Code: 451, Message: "Required malware scanner unavailable"}
		}
		return nil
	}
	if found {
		return &smtp.SMTPError{Code: 550, Message: "Malware detected"}
	}
	return nil
}

func countHeader(data []byte, name string) int {
	m, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	return len(m.Header[name])
}
