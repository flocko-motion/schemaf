package codegen

import (
	"encoding/json"
	"errors"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var semverRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// The generator and runtime are fetched at fixed release versions, never the
// newest (`go get <pkg>` without @, or @latest).
func TestOapiGetArgsArePinned(t *testing.T) {
	for name, v := range map[string]string{"oapiCodegenVersion": oapiCodegenVersion, "oapiRuntimeVersion": oapiRuntimeVersion} {
		if !semverRe.MatchString(v) {
			t.Errorf("%s = %q, want a release version vX.Y.Z", name, v)
		}
	}
	codegen, runtime := oapiGetArgs()
	for _, args := range [][]string{codegen, runtime} {
		if len(args) != 2 || args[0] != "get" {
			t.Fatalf("go get args = %q, want [get <pkg>@<version>]", args)
		}
	}
	if want := oapiCodegenPkg + "@" + oapiCodegenVersion; codegen[1] != want {
		t.Errorf("generator: go get %s, want %s", codegen[1], want)
	}
	if want := oapiRuntimePkg + "@" + oapiRuntimeVersion; runtime[1] != want {
		t.Errorf("runtime: go get %s, want %s", runtime[1], want)
	}
}

// The TypeScript client generator runs at an exact npm version: no range
// (^, ~), no tag (latest), no bare package name.
func TestApitsNpxArgsArePinned(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(swaggerTSAPIVersion) {
		t.Errorf("swaggerTSAPIVersion = %q, want an exact version X.Y.Z", swaggerTSAPIVersion)
	}
	args := apitsArgs()
	want := swaggerTSAPIPkg + "@" + swaggerTSAPIVersion
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, swaggerTSAPIPkg) {
			if a != want {
				t.Errorf("npx runs %q, want %q", a, want)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("npx args %q don't run %s", args, want)
	}
}

// Every module in the graph a new project gets (schemaf plus the pinned
// generator and runtime) must work on schemaf's minimum Go, the go directive
// of schemaf's go.mod: the CI gate runs exactly that Go with GOTOOLCHAIN=local.
// The go tool reads the graph's go.mod files from the module cache; on a fresh
// cache it downloads them with the caller's GOPROXY, and fails loudly if it can't.
func TestOapiPinsFitSchemafGo(t *testing.T) {
	checkout, err := filepath.Abs(schemafCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	minGo := ""
	for _, line := range strings.Split(readFile(t, filepath.Join(checkout, "go.mod")), "\n") {
		if v, ok := strings.CutPrefix(line, "go "); ok {
			minGo = strings.TrimSpace(v)
		}
	}
	if minGo == "" {
		t.Fatalf("%s/go.mod has no go directive", checkout)
	}

	dir := t.TempDir()
	gomod := "module schemaf.local/oapipins\n\ngo " + minGo + "\n\n" +
		"require (\n" +
		"\t" + schemafModule + " v0.0.0\n" +
		"\t" + oapiCodegenModule + " " + oapiCodegenVersion + "\n" +
		"\t" + oapiRuntimeModule + " " + oapiRuntimeVersion + "\n" +
		")\n\nreplace " + schemafModule + " => " + checkout + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "list", "-m", "-json", "all")
	cmd.Dir = dir
	// The pins are the minimum versions: -mod=mod lets go resolve the graph
	// without a go.sum. GOTOOLCHAIN=local keeps a dependency from switching
	// toolchains behind our back; this test compares the directives itself.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m all over schemaf + %s@%s + %s@%s failed (on a fresh module cache this needs the module proxy): %v\n%s",
			oapiCodegenModule, oapiCodegenVersion, oapiRuntimeModule, oapiRuntimeVersion, err, stderr.String())
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	seen := map[string]bool{}
	for {
		var m struct {
			Path      string
			Version   string
			GoVersion string
		}
		if err := dec.Decode(&m); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("parsing go list output: %v", err)
		}
		seen[m.Path] = true
		if m.GoVersion != "" && version.Compare("go"+m.GoVersion, "go"+minGo) > 0 {
			t.Errorf("%s@%s needs go %s, but schemaf's minimum is go %s (go.mod) — pin an older release", m.Path, m.Version, m.GoVersion, minGo)
		}
	}
	for _, mod := range []string{oapiCodegenModule, oapiRuntimeModule} {
		if !seen[mod] {
			t.Errorf("%s is missing from the module graph", mod)
		}
	}
}
