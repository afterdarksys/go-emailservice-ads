package routing

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)

// MappingEngine handles lookup configurations for access control,
// aliases, and routing rules typically found in traditional MTAs.
type MappingEngine struct {
	db *sql.DB
}

func NewMappingEngine(dbPath string) (*MappingEngine, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	return &MappingEngine{db: db}, nil
}

// CheckAccessMap returns the defined action for a network/IP key (e.g., "192.168.1.0/24", "REJECT").
// An empty string means no rule matches.
func (m *MappingEngine) CheckAccessMap(ip string) (string, error) {
	var action string
	// A robust implementation would handle full CIDR matching here.
	// For now, doing exact string match for IP subnets.
	err := m.db.QueryRow("SELECT action FROM access_map WHERE key = ?", ip).Scan(&action)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return action, err
}

// LookupMXRecord returns a hardcoded destination for a specific domain.
func (m *MappingEngine) LookupMXRecord(domain string) (string, error) {
	var dest string
	err := m.db.QueryRow("SELECT destination FROM mx_record_map WHERE domain = ?", domain).Scan(&dest)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return dest, err
}

// IsRelayDomain returns true if the domain is permitted to be relayed through this server.
func (m *MappingEngine) IsRelayDomain(domain string) (bool, error) {
	var action string
	err := m.db.QueryRow("SELECT action FROM relay_domains WHERE domain = ?", domain).Scan(&action)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return true, nil
}

// ExpandVirtualAlias takes an alias (e.g., "sales@msgs.global") and returns a slice of DIDs or actual email addresses.
func (m *MappingEngine) ExpandVirtualAlias(alias string) ([]string, error) {
	var destinations string
	err := m.db.QueryRow("SELECT destinations FROM virtual_alias_maps WHERE alias = ?", alias).Scan(&destinations)
	if err == sql.ErrNoRows {
		return nil, nil // No alias found
	}
	// Assuming comma-separated values in DB
	raw := strings.Split(destinations, ",")
	var result []string
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r != "" {
			result = append(result, r)
		}
	}
	return result, nil
}
