package codegen

import (
	"encoding/json"
	"os"
	"testing"
)

// TestOpenAPIIncludesAuthRoutes: the static gen/openapi.json always carries
// the framework auth routes, independent of deployment config.
func TestOpenAPIIncludesAuthRoutes(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeOpenAPIJSON(nil, map[string]structInfo{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(openapiJSONOut)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
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
