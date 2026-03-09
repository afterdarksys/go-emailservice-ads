package access

import (
	"context"
	"fmt"
	"net"
	"strings"

	"go.uber.org/zap"

	"github.com/afterdarksys/go-emailservice-ads/internal/access/maps"
)

// Manager manages access control restrictions
type Manager struct {
	logger *zap.Logger

	// Restriction lists for each stage
	clientRestrictions      []Restriction
	heloRestrictions        []Restriction
	senderRestrictions      []Restriction
	recipientRestrictions   []Restriction
	dataRestrictions        []Restriction
	endOfDataRestrictions   []Restriction
	etrnRestrictions        []Restriction

	// Restriction classes (named groups)
	classes map[string]*RestrictionClass

	// Map factory for lookups
	mapFactory *maps.Factory

	// Configuration
	myNetworks      []*net.IPNet
	myDomains       []string
	relayDomains    []string
}

// NewManager creates a new access control manager
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		logger:     logger,
		classes:    make(map[string]*RestrictionClass),
		mapFactory: maps.NewFactory(logger),
	}
}

// Configure configures the access control manager
func (m *Manager) Configure(config *Config) error {
	// Parse my networks
	for _, cidr := range config.MyNetworks {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid network %s: %w", cidr, err)
		}
		m.myNetworks = append(m.myNetworks, network)
	}

	m.myDomains = config.MyDomains
	m.relayDomains = config.RelayDomains

	// Build restrictions for each stage
	if err := m.buildRestrictions(StageClient, config.ClientRestrictions, &m.clientRestrictions); err != nil {
		return err
	}
	if err := m.buildRestrictions(StageHelo, config.HeloRestrictions, &m.heloRestrictions); err != nil {
		return err
	}
	if err := m.buildRestrictions(StageSender, config.SenderRestrictions, &m.senderRestrictions); err != nil {
		return err
	}
	if err := m.buildRestrictions(StageRecipient, config.RecipientRestrictions, &m.recipientRestrictions); err != nil {
		return err
	}
	if err := m.buildRestrictions(StageData, config.DataRestrictions, &m.dataRestrictions); err != nil {
		return err
	}
	if err := m.buildRestrictions(StageEndOfData, config.EndOfDataRestrictions, &m.endOfDataRestrictions); err != nil {
		return err
	}

	return nil
}

// Check evaluates restrictions for a given stage
func (m *Manager) Check(ctx context.Context, checkCtx *CheckContext) (*AccessDecision, error) {
	var restrictions []Restriction

	switch checkCtx.Stage {
	case StageClient:
		restrictions = m.clientRestrictions
	case StageHelo:
		restrictions = m.heloRestrictions
	case StageSender:
		restrictions = m.senderRestrictions
	case StageRecipient:
		restrictions = m.recipientRestrictions
	case StageData:
		restrictions = m.dataRestrictions
	case StageEndOfData:
		restrictions = m.endOfDataRestrictions
	case StageEtrn:
		restrictions = m.etrnRestrictions
	default:
		return nil, fmt.Errorf("unknown restriction stage: %s", checkCtx.Stage)
	}

	// Evaluate each restriction in order
	for _, restriction := range restrictions {
		decision, err := restriction.Check(ctx, checkCtx)
		if err != nil {
			m.logger.Error("Restriction check failed",
				zap.String("restriction", restriction.Name()),
				zap.Error(err))
			continue
		}

		// If not DUNNO, return the decision
		if decision.Result != ResultDunno {
			m.logger.Debug("Restriction matched",
				zap.String("restriction", restriction.Name()),
				zap.String("result", string(decision.Result)),
				zap.String("reason", decision.Reason))
			return decision, nil
		}
	}

	// Default: OK
	return &AccessDecision{
		Result: ResultOK,
		Reason: "default permit",
	}, nil
}

// buildRestrictions builds a list of restrictions from configuration
func (m *Manager) buildRestrictions(stage RestrictionStage, specs []string, dest *[]Restriction) error {
	for _, spec := range specs {
		restriction, err := m.parseRestriction(stage, spec)
		if err != nil {
			return fmt.Errorf("invalid restriction %s: %w", spec, err)
		}
		*dest = append(*dest, restriction)
	}
	return nil
}

// parseRestriction parses a restriction specification
func (m *Manager) parseRestriction(stage RestrictionStage, spec string) (Restriction, error) {
	// Split into restriction name and optional parameters
	parts := strings.Fields(spec)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty restriction")
	}

	name := parts[0]
	args := parts[1:]

	switch name {
	// Standard restrictions
	case "permit":
		return &PermitRestriction{stage: stage}, nil
	case "reject":
		return &RejectRestriction{stage: stage}, nil
	case "defer":
		return &DeferRestriction{stage: stage}, nil
	case "defer_if_permit":
		return &DeferIfPermitRestriction{stage: stage}, nil
	case "defer_if_reject":
		return &DeferIfRejectRestriction{stage: stage}, nil

	// Network restrictions
	case "permit_mynetworks":
		return &PermitMyNetworksRestriction{stage: stage, manager: m}, nil
	case "permit_sasl_authenticated":
		return &PermitSASLAuthenticatedRestriction{stage: stage}, nil

	// Relay restrictions
	case "reject_unauth_destination":
		return &RejectUnauthDestinationRestriction{stage: stage, manager: m}, nil
	case "permit_auth_destination":
		return &PermitAuthDestinationRestriction{stage: stage, manager: m}, nil

	// DNS restrictions
	case "reject_unknown_sender_domain":
		return &RejectUnknownSenderDomainRestriction{stage: stage}, nil
	case "reject_unknown_recipient_domain":
		return &RejectUnknownRecipientDomainRestriction{stage: stage}, nil
	case "reject_unknown_client_hostname":
		return &RejectUnknownClientHostnameRestriction{stage: stage}, nil

	// RBL restrictions
	case "reject_rbl_client":
		if len(args) == 0 {
			return nil, fmt.Errorf("reject_rbl_client requires RBL server argument")
		}
		return &RejectRBLClientRestriction{stage: stage, rblServer: args[0]}, nil
	case "reject_rhsbl_sender":
		if len(args) == 0 {
			return nil, fmt.Errorf("reject_rhsbl_sender requires RBL server argument")
		}
		return &RejectRHSBLSenderRestriction{stage: stage, rblServer: args[0]}, nil
	case "reject_rhsbl_recipient":
		if len(args) == 0 {
			return nil, fmt.Errorf("reject_rhsbl_recipient requires RBL server argument")
		}
		return &RejectRHSBLRecipientRestriction{stage: stage, rblServer: args[0]}, nil

	// Access map restrictions
	case "check_client_access":
		if len(args) == 0 {
			return nil, fmt.Errorf("check_client_access requires map argument")
		}
		accessMap, err := m.mapFactory.Create(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to create map: %w", err)
		}
		return &CheckClientAccessRestriction{stage: stage, accessMap: accessMap}, nil

	case "check_sender_access":
		if len(args) == 0 {
			return nil, fmt.Errorf("check_sender_access requires map argument")
		}
		accessMap, err := m.mapFactory.Create(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to create map: %w", err)
		}
		return &CheckSenderAccessRestriction{stage: stage, accessMap: accessMap}, nil

	case "check_recipient_access":
		if len(args) == 0 {
			return nil, fmt.Errorf("check_recipient_access requires map argument")
		}
		accessMap, err := m.mapFactory.Create(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to create map: %w", err)
		}
		return &CheckRecipientAccessRestriction{stage: stage, accessMap: accessMap}, nil

	case "check_helo_access":
		if len(args) == 0 {
			return nil, fmt.Errorf("check_helo_access requires map argument")
		}
		accessMap, err := m.mapFactory.Create(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to create map: %w", err)
		}
		return &CheckHeloAccessRestriction{stage: stage, accessMap: accessMap}, nil

	// Policy service
	case "check_policy_service":
		if len(args) == 0 {
			return nil, fmt.Errorf("check_policy_service requires service URL")
		}
		return &CheckPolicyServiceRestriction{stage: stage, serviceURL: args[0]}, nil

	// Restriction class reference
	default:
		// Check if it's a restriction class
		if class, ok := m.classes[name]; ok {
			return &RestrictionClassReference{stage: stage, class: class}, nil
		}

		return nil, fmt.Errorf("unknown restriction: %s", name)
	}
}

// Config holds access control configuration
type Config struct {
	MyNetworks     []string
	MyDomains      []string
	RelayDomains   []string

	ClientRestrictions      []string
	HeloRestrictions        []string
	SenderRestrictions      []string
	RecipientRestrictions   []string
	DataRestrictions        []string
	EndOfDataRestrictions   []string
}
