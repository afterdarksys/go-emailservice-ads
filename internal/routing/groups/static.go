package groups

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// StaticProvider handles static group membership
type StaticProvider struct {
	logger *zap.Logger
}

// NewStaticProvider creates a new static group provider
func NewStaticProvider(logger *zap.Logger) *StaticProvider {
	return &StaticProvider{
		logger: logger,
	}
}

// GetMembers returns the static list of members
func (sp *StaticProvider) GetMembers(ctx context.Context, group *Group) ([]string, error) {
	if group.Type != GroupTypeStatic {
		return nil, fmt.Errorf("invalid group type for static provider: %s", group.Type)
	}

	if group.Members == nil {
		return []string{}, nil
	}

	// Return a copy to prevent external modification
	members := make([]string, len(group.Members))
	copy(members, group.Members)

	sp.logger.Debug("Static group members retrieved",
		zap.Int("count", len(members)))

	return members, nil
}

// AddMember adds a member to a static group
func (sp *StaticProvider) AddMember(group *Group, email string) error {
	if group.Type != GroupTypeStatic {
		return fmt.Errorf("cannot add member to non-static group")
	}

	// Check if member already exists
	for _, member := range group.Members {
		if member == email {
			return fmt.Errorf("member already exists: %s", email)
		}
	}

	group.Members = append(group.Members, email)

	sp.logger.Info("Member added to static group",
		zap.String("email", email),
		zap.Int("total_members", len(group.Members)))

	return nil
}

// RemoveMember removes a member from a static group
func (sp *StaticProvider) RemoveMember(group *Group, email string) error {
	if group.Type != GroupTypeStatic {
		return fmt.Errorf("cannot remove member from non-static group")
	}

	found := false
	newMembers := make([]string, 0, len(group.Members))

	for _, member := range group.Members {
		if member != email {
			newMembers = append(newMembers, member)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("member not found: %s", email)
	}

	group.Members = newMembers

	sp.logger.Info("Member removed from static group",
		zap.String("email", email),
		zap.Int("total_members", len(group.Members)))

	return nil
}
