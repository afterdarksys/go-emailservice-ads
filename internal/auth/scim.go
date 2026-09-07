package auth

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

var ErrAccountConflict = errors.New("account already exists")

// ProvisionSCIM publishes identity and credentials together after persistence.
// An empty hash deliberately prevents password login until a separate credential
// workflow sets it. LDAP and federated tokens may authenticate the enabled user.
func (s *UserStore) ProvisionSCIM(username, email, external string, active bool) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[username]; exists {
		return nil, ErrAccountConflict
	}
	u := &User{Username: username, Email: email, Enabled: active, SCIMID: uuid.NewString(), ExternalID: external}
	if s.repository == nil {
		return nil, errors.New("SCIM requires persistent user storage")
	}
	if err := s.repository.SaveUser(context.Background(), u); err != nil {
		return nil, err
	}
	s.users[username] = u
	out := *u
	return &out, nil
}
func (s *UserStore) UpdateSCIM(id, email, external string, active bool) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, old := range s.users {
		if old.SCIMID == id {
			u := *old
			u.Email = email
			u.ExternalID = external
			u.Enabled = active
			if s.repository == nil {
				return nil, errors.New("SCIM requires persistent user storage")
			}
			if err := s.repository.SaveUser(context.Background(), &u); err != nil {
				return nil, err
			}
			s.users[name] = &u
			out := u
			return &out, nil
		}
	}
	return nil, ErrUserNotFound
}

// SecureDeleteUser is for an offline privacy operation. PostgreSQL physical
// maintenance remains the database operator's responsibility.
func (r *UserRepository) SecureDeleteUser(ctx context.Context, user string) error {
	if r.driver == "sqlite" {
		if _, err := r.db.ExecContext(ctx, "PRAGMA secure_delete=ON"); err != nil {
			return err
		}
	}
	if err := r.DeleteUser(ctx, user); err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}
	if r.driver == "sqlite" {
		if _, err := r.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return err
		}
		if _, err := r.db.ExecContext(ctx, "VACUUM"); err != nil {
			return err
		}
	}
	return nil
}
