package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/afterdarksys/go-emailservice-ads/internal/auth"
)

const scimUserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"

func scimError(w http.ResponseWriter, status int, kind, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"}, "status": strconv.Itoa(status), "scimType": kind, "detail": detail})
}
func scimObject(u auth.User) map[string]interface{} {
	return map[string]interface{}{"schemas": []string{scimUserSchema}, "id": u.SCIMID, "externalId": u.ExternalID, "userName": u.Username, "active": u.Enabled, "emails": []map[string]interface{}{{"value": u.Email, "primary": true}}, "meta": map[string]string{"resourceType": "User", "location": "/api/v1/scim/v2/Users/" + url.PathEscape(u.SCIMID)}}
}
func scimDecode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		scimError(w, 400, "invalidSyntax", "Invalid SCIM request")
		return false
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		scimError(w, 400, "invalidSyntax", "One JSON object required")
		return false
	}
	return true
}

type scimInput struct {
	Schemas    []string `json:"schemas"`
	UserName   string   `json:"userName"`
	ExternalID string   `json:"externalId"`
	Active     *bool    `json:"active"`
	Emails     []struct {
		Value   string `json:"value"`
		Primary bool   `json:"primary"`
		Type    string `json:"type"`
	} `json:"emails"`
}

func (s *Server) handleSCIM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.Header().Set("Cache-Control", "no-store")
	if s.userStore == nil {
		scimError(w, 503, "", "Account store unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/scim/v2/")
	if path == "ServiceProviderConfig" && r.Method == "GET" {
		json.NewEncoder(w).Encode(map[string]interface{}{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"}, "patch": map[string]bool{"supported": true}, "bulk": map[string]interface{}{"supported": false, "maxOperations": 0, "maxPayloadSize": 0}, "filter": map[string]interface{}{"supported": true, "maxResults": 200}, "changePassword": map[string]bool{"supported": false}, "sort": map[string]bool{"supported": false}, "etag": map[string]bool{"supported": false}, "authenticationSchemes": []map[string]interface{}{{"type": "oauthbearertoken", "name": "Scoped bearer token", "description": "scim:read or scim:write", "primary": true}}})
		return
	}
	if path != "Users" && !strings.HasPrefix(path, "Users/") {
		scimError(w, 404, "", "Resource not found")
		return
	}
	id := strings.TrimPrefix(path, "Users/")
	if path == "Users" {
		id = ""
	}
	users := s.userStore.ListUsers()
	var existing *auth.User
	for _, u := range users {
		if id != "" && u.SCIMID == id {
			copy := u
			existing = &copy
		}
	}
	if id != "" && existing == nil {
		scimError(w, 404, "", "User not found")
		return
	}
	switch r.Method {
	case "GET":
		if existing != nil {
			json.NewEncoder(w).Encode(scimObject(*existing))
			return
		}
		q := r.URL.Query()
		start, count := 1, 100
		for k := range q {
			if k != "filter" && k != "startIndex" && k != "count" {
				scimError(w, 400, "invalidValue", "Unsupported query parameter")
				return
			}
		}
		if q.Has("startIndex") {
			v, e := strconv.Atoi(q.Get("startIndex"))
			if e != nil || v < 1 {
				scimError(w, 400, "invalidValue", "Invalid startIndex")
				return
			}
			start = v
		}
		if q.Has("count") {
			v, e := strconv.Atoi(q.Get("count"))
			if e != nil || v < 0 {
				scimError(w, 400, "invalidValue", "Invalid count")
				return
			}
			count = v
		}
		if count > 200 {
			count = 200
		}
		field, value := "", ""
		if f := q.Get("filter"); f != "" {
			parts := strings.SplitN(f, " eq ", 2)
			if len(parts) != 2 || (parts[0] != "userName" && parts[0] != "externalId") || json.Unmarshal([]byte(parts[1]), &value) != nil {
				scimError(w, 400, "invalidFilter", "Supported filters: userName eq or externalId eq quoted value")
				return
			}
			field = parts[0]
		}
		matches := []map[string]interface{}{}
		for _, u := range users {
			if u.SCIMID == "" {
				continue
			}
			if field == "userName" && u.Username != value || field == "externalId" && u.ExternalID != value {
				continue
			}
			matches = append(matches, scimObject(u))
		}
		total := len(matches)
		offset := start - 1
		if offset > total {
			offset = total
		}
		end := offset + count
		if end > total {
			end = total
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"}, "totalResults": total, "startIndex": start, "itemsPerPage": end - offset, "Resources": matches[offset:end]})
	case "POST", "PUT":
		if (r.Method == "POST" && id != "") || (r.Method == "PUT" && id == "") {
			scimError(w, 405, "", "Method not allowed")
			return
		}
		var in scimInput
		if !scimDecode(w, r, &in) {
			return
		}
		if len(in.Schemas) != 1 || in.Schemas[0] != scimUserSchema || validateIdentifier("userName", in.UserName) != "" || len(in.ExternalID) > 255 || len(in.Emails) > 1 {
			scimError(w, 400, "invalidValue", "Invalid user attributes")
			return
		}
		email := in.UserName
		if len(in.Emails) == 1 {
			email = in.Emails[0].Value
		}
		a, err := mail.ParseAddress(email)
		if err != nil || a.Address != email {
			scimError(w, 400, "invalidValue", "Valid email required")
			return
		}
		active := true
		if in.Active != nil {
			active = *in.Active
		}
		var u *auth.User
		if existing == nil {
			u, err = s.userStore.ProvisionSCIM(in.UserName, email, in.ExternalID, active)
		} else {
			if in.UserName != existing.Username {
				scimError(w, 400, "mutability", "userName is immutable")
				return
			}
			u, err = s.userStore.UpdateSCIM(id, email, in.ExternalID, active)
		}
		if errors.Is(err, auth.ErrAccountConflict) {
			scimError(w, 409, "uniqueness", "User already exists")
			return
		}
		if err != nil {
			scimError(w, 500, "", "Unable to persist account")
			return
		}
		if existing == nil {
			w.Header().Set("Location", "/api/v1/scim/v2/Users/"+u.SCIMID)
			w.WriteHeader(201)
		}
		json.NewEncoder(w).Encode(scimObject(*u))
	case "PATCH":
		if existing == nil {
			scimError(w, 405, "", "User ID required")
			return
		}
		var in struct {
			Schemas    []string `json:"schemas"`
			Operations []struct {
				Op    string          `json:"op"`
				Path  string          `json:"path"`
				Value json.RawMessage `json:"value"`
			} `json:"Operations"`
		}
		if !scimDecode(w, r, &in) {
			return
		}
		if len(in.Schemas) != 1 || in.Schemas[0] != "urn:ietf:params:scim:api:messages:2.0:PatchOp" || len(in.Operations) == 0 || len(in.Operations) > 32 {
			scimError(w, 400, "invalidSyntax", "Invalid patch")
			return
		}
		active := existing.Enabled
		for _, op := range in.Operations {
			if !strings.EqualFold(op.Op, "replace") && !strings.EqualFold(op.Op, "add") {
				scimError(w, 400, "invalidPath", "Only active add/replace supported")
				return
			}
			if op.Path == "" {
				var values map[string]json.RawMessage
				if json.Unmarshal(op.Value, &values) != nil || len(values) != 1 || values["active"] == nil {
					scimError(w, 400, "invalidPath", "Only active supported")
					return
				}
				op.Value = values["active"]
			} else if !strings.EqualFold(op.Path, "active") {
				scimError(w, 400, "invalidPath", "Only active supported")
				return
			}
			if string(op.Value) == "null" || json.Unmarshal(op.Value, &active) != nil {
				scimError(w, 400, "invalidValue", "active must be boolean")
				return
			}
		}
		u, err := s.userStore.UpdateSCIM(id, existing.Email, existing.ExternalID, active)
		if err != nil {
			scimError(w, 500, "", "Unable to persist account")
			return
		}
		json.NewEncoder(w).Encode(scimObject(*u))
	case "DELETE":
		if existing == nil {
			scimError(w, 405, "", "User ID required")
			return
		}
		if err := s.userStore.DeleteUser(existing.Username); err != nil {
			scimError(w, 500, "", "Unable to delete account")
			return
		}
		w.WriteHeader(204)
	default:
		scimError(w, 405, "", "Method not allowed")
	}
}
