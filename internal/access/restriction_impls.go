package access

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// Basic restrictions

type PermitRestriction struct {
	stage RestrictionStage
}

func (r *PermitRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	return &AccessDecision{Result: ResultOK, Reason: "permit"}, nil
}
func (r *PermitRestriction) Name() string         { return "permit" }
func (r *PermitRestriction) Stage() RestrictionStage { return r.stage }

type RejectRestriction struct {
	stage RestrictionStage
}

func (r *RejectRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	return &AccessDecision{Result: ResultReject, Message: "Access denied", Code: 554}, nil
}
func (r *RejectRestriction) Name() string         { return "reject" }
func (r *RejectRestriction) Stage() RestrictionStage { return r.stage }

type DeferRestriction struct {
	stage RestrictionStage
}

func (r *DeferRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	return &AccessDecision{Result: ResultDefer, Message: "Service temporarily unavailable", Code: 450}, nil
}
func (r *DeferRestriction) Name() string         { return "defer" }
func (r *DeferRestriction) Stage() RestrictionStage { return r.stage }

// Network-based restrictions

type PermitMyNetworksRestriction struct {
	stage   RestrictionStage
	manager *Manager
}

func (r *PermitMyNetworksRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	ip := net.ParseIP(checkCtx.ClientAddr)
	if ip == nil {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	for _, network := range r.manager.myNetworks {
		if network.Contains(ip) {
			return &AccessDecision{Result: ResultOK, Reason: "client in mynetworks"}, nil
		}
	}

	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *PermitMyNetworksRestriction) Name() string         { return "permit_mynetworks" }
func (r *PermitMyNetworksRestriction) Stage() RestrictionStage { return r.stage }

type PermitSASLAuthenticatedRestriction struct {
	stage RestrictionStage
}

func (r *PermitSASLAuthenticatedRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Check if authenticated (from metadata)
	if auth, ok := checkCtx.Metadata["authenticated"].(bool); ok && auth {
		return &AccessDecision{Result: ResultOK, Reason: "SASL authenticated"}, nil
	}
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *PermitSASLAuthenticatedRestriction) Name() string         { return "permit_sasl_authenticated" }
func (r *PermitSASLAuthenticatedRestriction) Stage() RestrictionStage { return r.stage }

// Relay restrictions

type RejectUnauthDestinationRestriction struct {
	stage   RestrictionStage
	manager *Manager
}

func (r *RejectUnauthDestinationRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Check if authenticated
	if auth, ok := checkCtx.Metadata["authenticated"].(bool); ok && auth {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	// Check if client is in mynetworks
	ip := net.ParseIP(checkCtx.ClientAddr)
	if ip != nil {
		for _, network := range r.manager.myNetworks {
			if network.Contains(ip) {
				return &AccessDecision{Result: ResultDunno}, nil
			}
		}
	}

	// Check if recipient domain is local or relay domain
	if checkCtx.Recipient != "" {
		domain := extractDomain(checkCtx.Recipient)
		if isInList(domain, r.manager.myDomains) || isInList(domain, r.manager.relayDomains) {
			return &AccessDecision{Result: ResultDunno}, nil
		}
	}

	return &AccessDecision{
		Result:  ResultReject,
		Message: "Relay access denied",
		Code:    554,
		Reason:  "unauthorized destination",
	}, nil
}
func (r *RejectUnauthDestinationRestriction) Name() string         { return "reject_unauth_destination" }
func (r *RejectUnauthDestinationRestriction) Stage() RestrictionStage { return r.stage }

type PermitAuthDestinationRestriction struct {
	stage   RestrictionStage
	manager *Manager
}

func (r *PermitAuthDestinationRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	if checkCtx.Recipient != "" {
		domain := extractDomain(checkCtx.Recipient)
		if isInList(domain, r.manager.myDomains) || isInList(domain, r.manager.relayDomains) {
			return &AccessDecision{Result: ResultOK, Reason: "authorized destination"}, nil
		}
	}
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *PermitAuthDestinationRestriction) Name() string         { return "permit_auth_destination" }
func (r *PermitAuthDestinationRestriction) Stage() RestrictionStage { return r.stage }

// DNS-based restrictions

type RejectUnknownSenderDomainRestriction struct {
	stage RestrictionStage
}

func (r *RejectUnknownSenderDomainRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	if checkCtx.Sender == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	domain := extractDomain(checkCtx.Sender)
	if domain == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	// Check if domain has MX or A records
	mxRecords, err := net.LookupMX(domain)
	if err == nil && len(mxRecords) > 0 {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	ipRecords, err := net.LookupIP(domain)
	if err == nil && len(ipRecords) > 0 {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	return &AccessDecision{
		Result:  ResultReject,
		Message: fmt.Sprintf("Sender address rejected: Domain %s not found", domain),
		Code:    450,
		Reason:  "unknown sender domain",
	}, nil
}
func (r *RejectUnknownSenderDomainRestriction) Name() string         { return "reject_unknown_sender_domain" }
func (r *RejectUnknownSenderDomainRestriction) Stage() RestrictionStage { return r.stage }

type RejectUnknownRecipientDomainRestriction struct {
	stage RestrictionStage
}

func (r *RejectUnknownRecipientDomainRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	if checkCtx.Recipient == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	domain := extractDomain(checkCtx.Recipient)
	if domain == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	// Check if domain has MX or A records
	mxRecords, err := net.LookupMX(domain)
	if err == nil && len(mxRecords) > 0 {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	ipRecords, err := net.LookupIP(domain)
	if err == nil && len(ipRecords) > 0 {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	return &AccessDecision{
		Result:  ResultReject,
		Message: fmt.Sprintf("Recipient address rejected: Domain %s not found", domain),
		Code:    450,
		Reason:  "unknown recipient domain",
	}, nil
}
func (r *RejectUnknownRecipientDomainRestriction) Name() string { return "reject_unknown_recipient_domain" }
func (r *RejectUnknownRecipientDomainRestriction) Stage() RestrictionStage { return r.stage }

type RejectUnknownClientHostnameRestriction struct {
	stage RestrictionStage
}

func (r *RejectUnknownClientHostnameRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	if checkCtx.ClientName == "" {
		// Try reverse DNS lookup
		names, err := net.LookupAddr(checkCtx.ClientAddr)
		if err != nil || len(names) == 0 {
			return &AccessDecision{
				Result:  ResultReject,
				Message: "Client host rejected: cannot find your hostname",
				Code:    450,
				Reason:  "unknown client hostname",
			}, nil
		}
	}
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *RejectUnknownClientHostnameRestriction) Name() string { return "reject_unknown_client_hostname" }
func (r *RejectUnknownClientHostnameRestriction) Stage() RestrictionStage { return r.stage }

// RBL restrictions

type RejectRBLClientRestriction struct {
	stage     RestrictionStage
	rblServer string
}

func (r *RejectRBLClientRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Reverse IP and query RBL
	reversed := reverseIP(checkCtx.ClientAddr)
	query := fmt.Sprintf("%s.%s", reversed, r.rblServer)

	addrs, err := net.LookupHost(query)
	if err == nil && len(addrs) > 0 {
		return &AccessDecision{
			Result:  ResultReject,
			Message: fmt.Sprintf("Client host rejected: listed in %s", r.rblServer),
			Code:    554,
			Reason:  "RBL listed",
		}, nil
	}

	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *RejectRBLClientRestriction) Name() string         { return "reject_rbl_client" }
func (r *RejectRBLClientRestriction) Stage() RestrictionStage { return r.stage }

type RejectRHSBLSenderRestriction struct {
	stage     RestrictionStage
	rblServer string
}

func (r *RejectRHSBLSenderRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	domain := extractDomain(checkCtx.Sender)
	if domain == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	query := fmt.Sprintf("%s.%s", domain, r.rblServer)
	addrs, err := net.LookupHost(query)
	if err == nil && len(addrs) > 0 {
		return &AccessDecision{
			Result:  ResultReject,
			Message: fmt.Sprintf("Sender domain rejected: listed in %s", r.rblServer),
			Code:    554,
			Reason:  "RHSBL listed",
		}, nil
	}

	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *RejectRHSBLSenderRestriction) Name() string         { return "reject_rhsbl_sender" }
func (r *RejectRHSBLSenderRestriction) Stage() RestrictionStage { return r.stage }

type RejectRHSBLRecipientRestriction struct {
	stage     RestrictionStage
	rblServer string
}

func (r *RejectRHSBLRecipientRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	domain := extractDomain(checkCtx.Recipient)
	if domain == "" {
		return &AccessDecision{Result: ResultDunno}, nil
	}

	query := fmt.Sprintf("%s.%s", domain, r.rblServer)
	addrs, err := net.LookupHost(query)
	if err == nil && len(addrs) > 0 {
		return &AccessDecision{
			Result:  ResultReject,
			Message: fmt.Sprintf("Recipient domain rejected: listed in %s", r.rblServer),
			Code:    554,
			Reason:  "RHSBL listed",
		}, nil
	}

	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *RejectRHSBLRecipientRestriction) Name() string { return "reject_rhsbl_recipient" }
func (r *RejectRHSBLRecipientRestriction) Stage() RestrictionStage { return r.stage }

// Access map restrictions

type CheckClientAccessRestriction struct {
	stage     RestrictionStage
	accessMap Map
}

func (r *CheckClientAccessRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	result, err := r.accessMap.Lookup(ctx, checkCtx.ClientAddr)
	if err != nil {
		return &AccessDecision{Result: ResultDunno}, nil
	}
	return parseMapResult(result), nil
}
func (r *CheckClientAccessRestriction) Name() string         { return "check_client_access" }
func (r *CheckClientAccessRestriction) Stage() RestrictionStage { return r.stage }

type CheckSenderAccessRestriction struct {
	stage     RestrictionStage
	accessMap Map
}

func (r *CheckSenderAccessRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	result, err := r.accessMap.Lookup(ctx, checkCtx.Sender)
	if err != nil {
		return &AccessDecision{Result: ResultDunno}, nil
	}
	return parseMapResult(result), nil
}
func (r *CheckSenderAccessRestriction) Name() string         { return "check_sender_access" }
func (r *CheckSenderAccessRestriction) Stage() RestrictionStage { return r.stage }

type CheckRecipientAccessRestriction struct {
	stage     RestrictionStage
	accessMap Map
}

func (r *CheckRecipientAccessRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	result, err := r.accessMap.Lookup(ctx, checkCtx.Recipient)
	if err != nil {
		return &AccessDecision{Result: ResultDunno}, nil
	}
	return parseMapResult(result), nil
}
func (r *CheckRecipientAccessRestriction) Name() string { return "check_recipient_access" }
func (r *CheckRecipientAccessRestriction) Stage() RestrictionStage { return r.stage }

type CheckHeloAccessRestriction struct {
	stage     RestrictionStage
	accessMap Map
}

func (r *CheckHeloAccessRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	result, err := r.accessMap.Lookup(ctx, checkCtx.HeloName)
	if err != nil {
		return &AccessDecision{Result: ResultDunno}, nil
	}
	return parseMapResult(result), nil
}
func (r *CheckHeloAccessRestriction) Name() string         { return "check_helo_access" }
func (r *CheckHeloAccessRestriction) Stage() RestrictionStage { return r.stage }

// Policy service restriction

type CheckPolicyServiceRestriction struct {
	stage      RestrictionStage
	serviceURL string
}

func (r *CheckPolicyServiceRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// TODO: Implement external policy service protocol
	return &AccessDecision{Result: ResultDunno, Reason: "policy service not implemented"}, nil
}
func (r *CheckPolicyServiceRestriction) Name() string         { return "check_policy_service" }
func (r *CheckPolicyServiceRestriction) Stage() RestrictionStage { return r.stage }

// Deferred restrictions

type DeferIfPermitRestriction struct {
	stage RestrictionStage
}

func (r *DeferIfPermitRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Would need to track previous decision
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *DeferIfPermitRestriction) Name() string         { return "defer_if_permit" }
func (r *DeferIfPermitRestriction) Stage() RestrictionStage { return r.stage }

type DeferIfRejectRestriction struct {
	stage RestrictionStage
}

func (r *DeferIfRejectRestriction) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Would need to track previous decision
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *DeferIfRejectRestriction) Name() string         { return "defer_if_reject" }
func (r *DeferIfRejectRestriction) Stage() RestrictionStage { return r.stage }

// Restriction class reference

type RestrictionClassReference struct {
	stage RestrictionStage
	class *RestrictionClass
}

func (r *RestrictionClassReference) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	// Evaluate all restrictions in the class
	for _, restriction := range r.class.Restrictions {
		decision, err := restriction.Check(ctx, checkCtx)
		if err != nil {
			continue
		}
		if decision.Result != ResultDunno {
			return decision, nil
		}
	}
	return &AccessDecision{Result: ResultDunno}, nil
}
func (r *RestrictionClassReference) Name() string         { return r.class.Name }
func (r *RestrictionClassReference) Stage() RestrictionStage { return r.stage }

// Helper functions

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func isInList(item string, list []string) bool {
	for _, elem := range list {
		if elem == item {
			return true
		}
	}
	return false
}

func reverseIP(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])
	}
	return ip
}

func parseMapResult(result string) *AccessDecision {
	result = strings.TrimSpace(result)
	parts := strings.SplitN(result, " ", 2)
	action := parts[0]
	message := ""
	if len(parts) > 1 {
		message = parts[1]
	}

	switch strings.ToUpper(action) {
	case "OK":
		return &AccessDecision{Result: ResultOK, Reason: "map returned OK"}
	case "REJECT":
		return &AccessDecision{Result: ResultReject, Message: message, Code: 554}
	case "DEFER":
		return &AccessDecision{Result: ResultDefer, Message: message, Code: 450}
	case "DUNNO":
		return &AccessDecision{Result: ResultDunno}
	case "DISCARD":
		return &AccessDecision{Result: ResultDiscard}
	case "HOLD":
		return &AccessDecision{Result: ResultHold}
	default:
		// Check for 4xx/5xx codes
		if len(action) == 3 && (action[0] == '4' || action[0] == '5') {
			code := 554
			fmt.Sscanf(action, "%d", &code)
			return &AccessDecision{Result: ResultReject, Message: message, Code: code}
		}
		return &AccessDecision{Result: ResultDunno}
	}
}
