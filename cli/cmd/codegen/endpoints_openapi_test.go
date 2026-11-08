package codegen

import (
	"encoding/json"
	"os"
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
	t.Chdir(t.TempDir())
	if err := writeOpenAPIJSON("myapp", endpoints, map[string]structInfo{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(openapiJSONOut)
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
		_, has401 := op["responses"].(map[string]any)["401"]
		if has401 != (w != "null") {
			t.Errorf("%s: 401 present = %v", key, has401)
		}
	}
}
