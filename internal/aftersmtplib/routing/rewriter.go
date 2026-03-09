package routing

import (
	"log"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/aftersmtplib/protocol/amp"
)

// Rewriter provides functions to translate headers and simulate aliases
// between the legacy ESMTP world and the AfterSMTP AMP E2EE world.
type Rewriter struct {
	mapEngine *MappingEngine
}

func NewRewriter(engine *MappingEngine) *Rewriter {
	return &Rewriter{
		mapEngine: engine,
	}
}

// IngressVirtualAliasExpansion handles inbound mail from standard SMTP.
// It looks up the RCPT TO address in the virtual_alias_maps.
// If aliases are found (e.g., sales@ -> alice_did, bob_did), it returns the expanded list.
// If none are found, it returns the original email address, assuming it maps natively to a DID.
func (r *Rewriter) IngressVirtualAliasExpansion(recipient string) []string {
	// Normalize recipient to just the email
	parts := strings.Split(recipient, "<")
	rcpt := parts[0]
	if len(parts) > 1 {
		rcpt = strings.TrimSuffix(parts[1], ">")
	}

	aliases, err := r.mapEngine.ExpandVirtualAlias(rcpt)
	if err != nil {
		log.Printf("[Rewriter] DB Error on Virtual Alias lookup for %s: %v", rcpt, err)
		return []string{rcpt}
	}

	if len(aliases) > 0 {
		return aliases
	}

	return []string{rcpt}
}

// EgressMapFromHeader is used when an AMP message is leaving the secure network
// bound for the legacy SMTP world (e.g., via the legacy.Bridge).
// It scrubs the DID information and tries to map it back to a standard
// standard internet email address From: header.
func (r *Rewriter) EgressMapFromHeader(msg *amp.AMPMessage) string {
	senderDID := msg.Headers.SenderDid

	// A real implementation would have a "canonical_maps" lookup here
	// mapping DIDs -> email addresses.
	// For this Phase, we do a naive string rewrite assuming the DID domain is the email domain.

	// e.g. did:aftersmtp:msgs.global:ryan   ->   ryan@msgs.global
	parts := strings.Split(senderDID, ":")
	if len(parts) >= 4 && parts[0] == "did" && parts[1] == "aftersmtp" {
		domain := parts[2]
		user := parts[3]
		return user + "@" + domain
	}

	// Fallback, couldn't parse the DID
	return senderDID
}
