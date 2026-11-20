package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

// runEmptyInputSteps runs the codegen steps that have empty-input variants,
// then writes gen/empty-inputs, in the order `codegen` runs them.
func runEmptyInputSteps(t *testing.T) {
	t.Helper()
	emptyInputs = nil
	if err := runMigrationsGen(nil); err != nil {
		t.Fatal(err)
	}
	if err := runSQLC(nil); err != nil {
		t.Fatal(err)
	}
	if err := runEndpointsGen(nil); err != nil {
		t.Fatal(err)
	}
	if err := runEmptyInputsGen(); err != nil {
		t.Fatal(err)
	}
}

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEmptyInputsEmptyProject(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"schemaf.toml": "title = \"My App\"\nname = \"myapp\"\n",
		"go/go.mod":    "module myapp\n\ngo 1.24\n\nrequire github.com/flocko-motion/schemaf v1.11.0\n",
	})
	runEmptyInputSteps(t)

	want := "no migrations in go/db/migrations — db.Provider() is empty\n" +
		"no queries in go/db/queries — no query code generated\n" +
		"no endpoints in go/api — Provider() registers nothing\n"
	if got := readFile(t, emptyInputsFile); got != want {
		t.Errorf("%s =\n%s\nwant\n%s", emptyInputsFile, got, want)
	}
	if _, err := os.Stat("go/db/migrations.gen.go"); err != nil {
		t.Errorf("no migrations.gen.go generated for an empty project: %v", err)
	}
}

func TestEmptyInputsFullProject(t *testing.T) {
	// Codegen type-checks go/api, so the module must resolve.
	gomod, gosum := offlineGoMod(t, "myapp", "v1.11.0", nil)
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"schemaf.toml":                   "title = \"My App\"\nname = \"myapp\"\n",
		"go/go.mod":                      gomod,
		"go/go.sum":                      gosum,
		"go/db/migrations/0001_init.sql": "CREATE TABLE t (id int);\n",
		"go/db/queries/t.sql":            "-- name: ListT :many\nSELECT * FROM t;\n",
		"go/api/ping.go": `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

type PingEndpoint struct{}

func (PingEndpoint) Method() string                { return "GET" }
func (PingEndpoint) Path() string                  { return "/api/ping" }
func (PingEndpoint) Auth() schemafapi.AuthLevel    { return schemafapi.AuthNone }
func (PingEndpoint) Handle(ctx context.Context, req PingReq) (PingResp, error) { return PingResp{}, nil }

type PingReq struct{}
type PingResp struct{}
`,
	})
	// A stale list from an earlier run is replaced.
	writeFiles(t, map[string]string{emptyInputsFile: "no endpoints in go/api — Provider() registers nothing\n"})
	runEmptyInputSteps(t)

	if got := readFile(t, emptyInputsFile); got != "" {
		t.Errorf("%s = %q for a project with no empty inputs, want empty", emptyInputsFile, got)
	}
}
