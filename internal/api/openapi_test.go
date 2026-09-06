package api

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/afterdarksys/go-emailservice-ads/internal/bounce"
	"github.com/afterdarksys/go-emailservice-ads/internal/compliance"
	"github.com/afterdarksys/go-emailservice-ads/internal/mailstorm"
	"github.com/afterdarksys/go-emailservice-ads/internal/policy"
	"github.com/afterdarksys/go-emailservice-ads/internal/storage"
	"github.com/afterdarksys/go-emailservice-ads/internal/version"
	openapiv3 "github.com/google/gnostic-models/openapiv3"
)

func loadContract(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openapiv3.ParseDocument(raw); err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func contractRef(t *testing.T, doc map[string]any, ref string) map[string]any {
	t.Helper()
	var node any = doc
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("nonlocal contract reference %s", ref)
	}
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("invalid reference %s", ref)
		}
		node = m[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
	}
	result, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("unresolved reference %s", ref)
	}
	return result
}
func TestOpenAPIValidReferencesAndVersion(t *testing.T) {
	doc := loadContract(t)
	if doc["info"].(map[string]any)["version"] != version.Version {
		t.Fatal("OpenAPI version differs from executable")
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "$ref" {
					contractRef(t, doc, item.(string))
				}
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(doc)
}

// Read registrations from the real buildMux, then match each documented path
// through that mux. Both a newly added route and an orphaned contract path fail.
func TestOpenAPIRoutesAndPermissions(t *testing.T) {
	doc := loadContract(t)
	source, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, decl := range source.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "buildMux" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				t.Fatal("route registration is not a literal; update contract checker")
			}
			route, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			registered[route] = false
			return true
		})
	}
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"unrelated:read"}
	mux := s.buildMux()
	ids := map[string]bool{}
	variable := regexp.MustCompile(`\{([^}]+)\}`)
	for path, value := range doc["paths"].(map[string]any) {
		item := value.(map[string]any)
		requestPath := variable.ReplaceAllString(path, "contract-probe")
		for method, raw := range item {
			if method == "parameters" {
				continue
			}
			operation := raw.(map[string]any)
			operationID := operation["operationId"].(string)
			if ids[operationID] {
				t.Errorf("duplicate operationId %s", operationID)
			}
			ids[operationID] = true
			req := httptest.NewRequest(strings.ToUpper(method), requestPath, nil)
			_, pattern := mux.Handler(req)
			if _, ok := registered[pattern]; !ok {
				t.Errorf("%s %s has no registered route", method, path)
				continue
			}
			registered[pattern] = true
			scope := operation["x-required-permission"].(string)
			if scope == "public" {
				security, ok := operation["security"].([]any)
				if !ok || len(security) != 0 {
					t.Errorf("public path %s must override bearer security", path)
				}
				continue
			}
			if got := requiredScope(req); got != scope {
				t.Errorf("%s %s scope: code=%s contract=%s", method, path, got, scope)
			}
			for _, tc := range []struct {
				key    string
				status int
			}{{"", 401}, {testAPIKey, 403}} {
				rec := doMailboxRequest(s, strings.ToUpper(method), requestPath, tc.key, nil)
				if rec.Code != tc.status {
					t.Errorf("%s %s auth=%d want %d", method, path, rec.Code, tc.status)
				}
			}
			for _, name := range variable.FindAllStringSubmatch(path, -1) {
				found := false
				for _, p := range item["parameters"].([]any) {
					param := p.(map[string]any)
					if param["name"] == name[1] && param["in"] == "path" && param["required"] == true {
						found = true
					}
				}
				if !found {
					t.Errorf("missing required path parameter %s in %s", name[1], path)
				}
			}
		}
	}
	for route, covered := range registered {
		if !covered {
			t.Errorf("registered route %s is missing from OpenAPI", route)
		}
	}
}

// A new/renamed Go JSON field must be reflected in its named contract schema.
func TestOpenAPIJSONFieldDrift(t *testing.T) {
	doc := loadContract(t)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, value := range map[string]any{
		"MailboxCreate": mailboxCreateRequest{}, "MailboxUpdate": mailboxUpdateRequest{}, "Mailbox": mailboxResponse{},
		"Policy": policy.PolicyConfig{}, "PolicyScope": policy.PolicyScope{}, "PolicyAction": policy.Action{},
		"JournalEntry": storage.JournalEntry{}, "BounceConfig": bounce.Config{}, "ComplianceRule": compliance.Rule{}, "Mailstorm": mailstorm.Status{},
	} {
		typ := reflect.TypeOf(value)
		properties := schemas[name].(map[string]any)["properties"].(map[string]any)
		fields := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			if key == "-" {
				continue
			}
			if key == "" {
				key = f.Name
			}
			fields[key] = true
			if _, ok := properties[key]; !ok {
				t.Errorf("%s missing JSON field %s", name, key)
			}
		}
		for key := range properties {
			if !fields[key] {
				t.Errorf("%s documents nonexistent field %s", name, key)
			}
		}
	}
	create := schemas["MailboxCreate"].(map[string]any)["properties"].(map[string]any)
	password := create["password"].(map[string]any)
	if password["x-minBytes"] != float64(minPasswordLen) || password["x-maxBytes"] != float64(maxPasswordLen) {
		t.Fatal("password byte constraints drifted")
	}
}

// Validate the response-schema subset used by this contract against actual
// handler output. OpenAPI syntax is checked separately by gnostic above.
func checkContractValue(doc map[string]any, schema map[string]any, value any) error {
	if ref, ok := schema["$ref"].(string); ok {
		schema = doc["components"].(map[string]any)["schemas"].(map[string]any)[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
	}
	if value == nil {
		if schema["nullable"] == true {
			return nil
		}
		return fmt.Errorf("unexpected null")
	}
	if enums, ok := schema["enum"].([]any); ok {
		found := false
		for _, v := range enums {
			found = found || reflect.DeepEqual(v, value)
		}
		if !found {
			return fmt.Errorf("unexpected enum %v", value)
		}
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
		for _, r := range anySlice(schema["required"]) {
			if _, ok := object[r.(string)]; !ok {
				return fmt.Errorf("missing %s", r)
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for key, v := range object {
			if p, ok := props[key].(map[string]any); ok {
				if err := checkContractValue(doc, p, v); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			} else if schema["additionalProperties"] == false {
				return fmt.Errorf("unexpected property %s", key)
			}
		}
	case "array":
		a, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		for _, v := range a {
			if err := checkContractValue(doc, schema["items"].(map[string]any), v); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		v, ok := value.(float64)
		if !ok || v != float64(int64(v)) {
			return fmt.Errorf("expected integer")
		}
	}
	return nil
}
func anySlice(value any) []any { v, _ := value.([]any); return v }
func TestOpenAPIHandlerResponses(t *testing.T) {
	doc := loadContract(t)
	s, _ := newMailboxTestServer(t, false)
	s.config.API.APIKeys[0].Permissions = []string{"*"}
	store, err := storage.NewMessageStore(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.store = store
	manager, err := policy.NewManager(&policy.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.UsePersistentFile(filepath.Join(t.TempDir(), "policies.yaml")); err != nil {
		t.Fatal(err)
	}
	s.policyMgr = manager
	for _, tc := range []struct {
		method, path, template string
		body                   any
		status                 int
	}{
		{"GET", "/health", "/health", nil, 200}, {"GET", "/api/v1/version", "/api/v1/version", nil, 200}, {"GET", "/ready", "/ready", nil, 503},
		{"POST", "/api/v1/mailboxes", "/api/v1/mailboxes", map[string]any{"username": "contract@example.test", "password": "contract-password"}, 201},
		{"GET", "/api/v1/mailboxes", "/api/v1/mailboxes", nil, 200},
		{"GET", "/api/v1/mailboxes/contract%40example.test", "/api/v1/mailboxes/{username}", nil, 200},
		{"PUT", "/api/v1/mailboxes/contract%40example.test", "/api/v1/mailboxes/{username}", map[string]any{"enabled": false}, 200},
		{"POST", "/api/v1/policies", "/api/v1/policies", map[string]any{"name": "contract", "type": "starlark", "enabled": false, "scope": map[string]any{"type": "global"}, "script": "reject(\"contract test\")"}, 201},
		{"GET", "/api/v1/policies/contract", "/api/v1/policies/{name}", nil, 200},
		{"POST", "/api/v1/policies/contract/test", "/api/v1/policies/{name}/test", map[string]any{"From": "a@test", "To": []string{"b@test"}, "Subject": "test", "Body": "body"}, 200},
		{"GET", "/api/v1/queue/pending", "/api/v1/queue/pending", nil, 200},
		{"GET", "/api/v1/dlq/list", "/api/v1/dlq/list", nil, 200},
		{"GET", "/api/v1/bounce/reports", "/api/v1/bounce/reports", nil, 200},
		{"GET", "/api/v1/policies", "/api/v1/policies", nil, 200}, {"GET", "/api/v1/policies/stats", "/api/v1/policies/stats", nil, 200},
		{"GET", "/api/v1/bounce/config", "/api/v1/bounce/config", nil, 200},
		{"DELETE", "/api/v1/mailboxes/contract%40example.test", "/api/v1/mailboxes/{username}", nil, 200},
	} {
		rec := doMailboxRequest(s, tc.method, tc.path, testAPIKey, tc.body)
		if rec.Code != tc.status {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		operation := doc["paths"].(map[string]any)[tc.template].(map[string]any)[strings.ToLower(tc.method)].(map[string]any)
		response := operation["responses"].(map[string]any)[strconv.Itoa(rec.Code)].(map[string]any)
		schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Fatal("content type drift")
		}
		var value any
		if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if err := checkContractValue(doc, schema, value); err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
		}
	}
}
