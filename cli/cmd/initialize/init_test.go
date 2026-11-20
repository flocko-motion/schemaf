package initialize

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/internal/gotest"
)

// The scaffolded wiring compiles: go/main.go (three lines), go/app/app.go
// (Wire) and go/tests/health_test.go (go vet type-checks the test), against
// stand-ins for what codegen generates.
func TestScaffoldedWiringCompiles(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	checkout := filepath.Join(filepath.Dir(file), "..", "..", "..")
	dir := t.TempDir()
	data := map[string]any{"Name": "compiletest"}

	files := map[string]string{
		"go.mod":               "module schemaf.local/compiletest\n\ngo 1.25.0\n\nrequire github.com/flocko-motion/schemaf v0.0.0\n\nreplace github.com/flocko-motion/schemaf => " + checkout + "\n",
		"api/endpoints.gen.go": "package api\n\nfunc Provider() {}\n",
		"db/migrations.gen.go": "package db\n\nimport \"embed\"\n\nfunc Provider() embed.FS { return embed.FS{} }\n",
		"app/frontend.gen.go":  "package app\n\nimport \"io/fs\"\n\nfunc FrontendFS() fs.FS { return nil }\n",
	}
	gosum, err := os.ReadFile(filepath.Join(checkout, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	files["go.sum"] = string(gosum)
	for tmpl, out := range map[string]string{
		"templates/main.go.tmpl":        "main.go",
		"templates/app.go.tmpl":         "app/app.go",
		"templates/health_test.go.tmpl": "tests/health_test.go",
	} {
		if err := renderFile(tmpl, filepath.Join(dir, out), data); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if main, _ := os.ReadFile(filepath.Join(dir, "main.go")); strings.Contains(string(main), "AddDb") {
		t.Error("main.go still wires providers; they belong in app.Wire")
	}
	gotest.RequireModuleCache(t) // the go tool runs offline below
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
