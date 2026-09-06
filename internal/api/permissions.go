package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

type principalKey struct{}

func requiredScope(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	resource := strings.SplitN(path, "/", 2)[0]
	switch resource {
	case "message", "dlq":
		resource = "queue"
	case "recipients":
		resource = "recipients"
	case "mailboxes":
		resource = "mailboxes"
	case "replication":
		resource = "replication"
	case "mailstorm":
		resource = "mailstorm"
	case "quarantine":
		resource = "quarantine"
	case "policies":
		resource = "policies"
	default:
		resource = "queue"
	}
	access := "write"
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		access = "read"
	}
	// Legacy mutating GET endpoints still require write authority.
	if strings.Contains(path, "/retry/") || strings.HasSuffix(path, "/reload") || strings.HasSuffix(path, "/promote") {
		access = "write"
	}
	return resource + ":" + access
}
func (s *Server) authorizeKey(token, scope string) (string, bool) {
	for _, key := range s.config.API.APIKeys {
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(key.Key)) != 1 {
			continue
		}
		for _, permission := range key.Permissions {
			if permission == "*" || permission == scope {
				return key.Name, true
			}
		}
	}
	return "", false
}
func principal(r *http.Request) string { s, _ := r.Context().Value(principalKey{}).(string); return s }
func withPrincipal(r *http.Request, name string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, name))
}
