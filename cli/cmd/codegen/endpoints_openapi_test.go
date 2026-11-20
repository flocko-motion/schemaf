package codegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

type staticSpec struct {
	Paths      map[string]map[string]map[string]any `json:"paths"`
	Components struct {
		SecuritySchemes map[string]map[string]any `json:"securitySchemes"`
		Schemas         map[string]any            `json:"schemas"`
	} `json:"components"`
}

func writeAndReadSpec(t *testing.T, endpoints []endpointInfo) staticSpec {
	t.Helper()
	raw, err := buildOpenAPISpec("myapp", endpoints, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var spec staticSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

// TestOpenAPIIncludesAuthRoutes: the static gen/openapi.json always carries
// the framework auth routes, independent of deployment config.
func TestOpenAPIIncludesAuthRoutes(t *testing.T) {
	spec := writeAndReadSpec(t, nil)
	for _, p := range []string{"/api/auth/auth0/login", "/api/auth/{provider}/login", "/api/auth/logout", "/api/auth/refresh"} {
		if _, ok := spec.Paths[p]["post"]; !ok {
			t.Errorf("missing POST %s", p)
		}
	}
	for _, s := range []string{"SchemafAuth0LoginRequest", "SchemafLoginResponse", "SchemafRefreshResponse"} {
		if _, ok := spec.Components.Schemas[s]; !ok {
			t.Errorf("missing schema %s", s)
		}
	}
}

// TestOpenAPISecurityPerLevel: cookieAuth scheme, and security per level.
func TestOpenAPISecurityPerLevel(t *testing.T) {
	spec := writeAndReadSpec(t, []endpointInfo{
		{StructName: "A", Method: "GET", Path: "/api/none", Auth: api.AuthNone, IsRaw: true},
		{StructName: "B", Method: "GET", Path: "/api/optional", Auth: api.AuthOptional, IsRaw: true},
		{StructName: "C", Method: "GET", Path: "/api/required", Auth: api.AuthRequired, IsRaw: true},
	})
	if s := spec.Components.SecuritySchemes["cookieAuth"]; s["type"] != "apiKey" || s["in"] != "cookie" || s["name"] != "myapp_session" {
		t.Errorf("cookieAuth scheme = %v", s)
	}
	if s := spec.Components.SecuritySchemes["bearerAuth"]; s["type"] != "http" || s["scheme"] != "bearer" {
		t.Errorf("bearerAuth scheme = %v", s)
	}
	want := map[string]string{
		"get /api/none":          "null",
		"get /api/optional":      `[{},{"bearerAuth":[]},{"cookieAuth":[]}]`,
		"get /api/required":      `[{"bearerAuth":[]},{"cookieAuth":[]}]`,
		"post /api/auth/refresh": `[{"bearerAuth":[]},{"cookieAuth":[]}]`,
	}
	for key, w := range want {
		var method, path string
		for i := range key {
			if key[i] == ' ' {
				method, path = key[:i], key[i+1:]
				break
			}
		}
		op := spec.Paths[path][method]
		if op == nil {
			t.Errorf("%s missing", key)
			continue
		}
		sec, _ := json.Marshal(op["security"])
		if string(sec) != w {
			t.Errorf("%s security = %s, want %s", key, sec, w)
		}
		checkDefaultErrorResponse(t, key, op)
	}
}

// TestOpenAPIErrorResponses: one shared Error schema, and every operation,
// raw and auth routes included, has a default response referencing it
// instead of description-only 400/401/500 entries.
func TestOpenAPIErrorResponses(t *testing.T) {
	spec := writeAndReadSpec(t, []endpointInfo{
		{StructName: "A", Method: "GET", Path: "/api/none", Auth: api.AuthNone, IsRaw: true},
		{StructName: "C", Method: "POST", Path: "/api/required", Auth: api.AuthRequired, IsRaw: true},
	})
	checkErrorSchema(t, spec.Components.Schemas)
	n := 0
	for path, item := range spec.Paths {
		for method, op := range item {
			n++
			checkDefaultErrorResponse(t, method+" "+path, op)
		}
	}
	if n < 6 {
		t.Errorf("only %d operations checked", n)
	}
}

// TestOpenAPIReservedSchemaName: an endpoint type named like a framework
// schema is a codegen error, never a silent overwrite.
func TestOpenAPIReservedSchemaName(t *testing.T) {
	schemas := map[string]any{
		"Error": map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
	}
	_, err := buildOpenAPISpec("myapp", []endpointInfo{
		{StructName: "G", Method: "GET", Path: "/api/g", Auth: api.AuthNone, ReqType: "GetReq", RespType: "Error",
			RespSchema: map[string]any{"$ref": "#/components/schemas/Error"}},
	}, schemas)
	if err == nil || !strings.Contains(err.Error(), `"Error"`) || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("err = %v, want a reserved-name error naming Error", err)
	}
}

func checkErrorSchema(t *testing.T, schemas map[string]any) {
	t.Helper()
	s, _ := schemas["Error"].(map[string]any)
	if s == nil {
		t.Fatal("components.schemas.Error missing")
	}
	req, _ := json.Marshal(s["required"])
	if string(req) != `["error","code"]` {
		t.Errorf("Error.required = %s", req)
	}
	props, _ := s["properties"].(map[string]any)
	for _, p := range []string{"error", "code", "id"} {
		if pp, _ := props[p].(map[string]any); pp["type"] != "string" {
			t.Errorf("Error.properties.%s = %v", p, props[p])
		}
	}
	code, _ := props["code"].(map[string]any)
	if d, _ := code["description"].(string); !strings.Contains(d, "too_many_requests") || !strings.Contains(d, "internal") {
		t.Errorf("Error.code description doesn't list the default codes: %q", d)
	}
	if _, hasEnum := code["enum"]; hasEnum {
		t.Error("Error.code has an enum; app codes are an open set")
	}
}

func checkDefaultErrorResponse(t *testing.T, name string, op map[string]any) {
	t.Helper()
	resps, _ := op["responses"].(map[string]any)
	def, _ := json.Marshal(resps["default"])
	if !strings.Contains(string(def), `"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}`) {
		t.Errorf("%s: default response = %s", name, def)
	}
	for code := range resps {
		if code != "default" && code[0] != '2' {
			t.Errorf("%s: has a %s response besides default", name, code)
		}
	}
}
