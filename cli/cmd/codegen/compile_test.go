package codegen

// compileGenerated is the shared real-compile check for generated Go files
// (EXTEND.md › Code Generation › Empty inputs). It runs the go tool because
// go:embed pattern errors are only reported by the compiler, not go/types.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/internal/gotest"
)

// compileModule is the module path of the temporary module; generated files
// that import project packages use it as {{.Module}}.
const compileModule = "schemaf.local/compiletest"

// schemafCheckout returns the root of the schemaf checkout these tests run in.
func schemafCheckout(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the schemaf checkout")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.HasPrefix(string(gomod), "module "+schemafModule+"\n") {
		t.Fatalf("%s is not the schemaf checkout", root)
	}
	return root
}

// compileGenerated writes files (paths relative to the module root; a path
// ending in "/" creates an empty directory) into a temporary Go module that
// requires the schemaf checkout via a replace directive, then runs go build
// and go vet on all of it. Either failing fails the test.
func compileGenerated(t *testing.T, files map[string]string) {
	t.Helper()
	checkout := schemafCheckout(t)
	gotest.RequireModuleCache(t)
	dir := t.TempDir()

	goVersion := ""
	gomod := readFile(t, filepath.Join(checkout, "go.mod"))
	for _, line := range strings.Split(gomod, "\n") {
		if v, ok := strings.CutPrefix(line, "go "); ok {
			goVersion = strings.TrimSpace(v)
		}
	}
	if goVersion == "" {
		t.Fatalf("%s/go.mod has no go directive", checkout)
	}
	files["go.mod"] = "module " + compileModule + "\n\ngo " + goVersion + "\n\n" +
		"require " + schemafModule + " v0.0.0\n\n" +
		"replace " + schemafModule + " => " + checkout + "\n"
	// schemaf's go.sum covers every module the generated code can reach.
	files["go.sum"] = readFile(t, filepath.Join(checkout, "go.sum"))

	for path, content := range files {
		full := filepath.Join(dir, path)
		if strings.HasSuffix(path, "/") {
			if err := os.MkdirAll(full, 0755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		// -mod=mod lets go add schemaf's indirect requirements to the
		// temporary go.mod; GOPROXY=off keeps the check offline (everything
		// is in the module cache: gotest.RequireModuleCache downloaded it).
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			var srcs strings.Builder
			for path, content := range files {
				if strings.HasSuffix(path, ".go") {
					srcs.WriteString("--- " + path + "\n" + content + "\n")
				}
			}
			t.Fatalf("go %s failed: %v\n%s\n%s", strings.Join(args, " "), err, out, srcs.String())
		}
	}
}

// mainStub makes a package main buildable when the generated file under test
// declares no func main.
const mainStub = "package main\n\nfunc main() {}\n"
