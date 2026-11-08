package codegen

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// containerCall matches a container runtime CLI in command position — at the
// start of a command, after a separator, `then`/`do`/`exec`, or inside $( ) —
// either by name or as "$rt". Messages that merely name a runtime don't match.
var containerCall = regexp.MustCompile(`(^|[;&|({]|\bthen|\bdo|\bexec|\$\()\s*("?\$\{?rt\}?"?|docker|podman)(\s|$)`)

// outsideRuntimeFunc returns the script's code lines that lie outside the
// _schemaf_container function, with comments stripped, and whether the
// function was defined.
func outsideRuntimeFunc(script string) (lines []string, sawFunc bool) {
	inFunc := false
	for _, line := range strings.Split(script, "\n") {
		switch {
		case strings.HasPrefix(line, "_schemaf_container() {"):
			inFunc, sawFunc = true, true
			continue
		case inFunc && line == "}":
			inFunc = false
			continue
		case inFunc:
			continue
		}
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sawFunc
}

// TestGeneratedScriptsUseRuntimeFunction renders schemaf.sh and gen/test-env.sh
// and asserts that no container call bypasses _schemaf_container.
func TestGeneratedScriptsUseRuntimeFunction(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("gen", 0755); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"Name": "myapp", "Port": 8000, "FrontendPort": 8002, "PostgresPort": 8003,
		"SchemafVersion": "v1.0.0", "InputGlobs": "go/api/*.go", "InputExcludes": "go/api/*.gen.go",
		"ExtServices": []ExtService{{Name: "clock", EnvVar: "CLOCK_URL", Ports: []string{"9000"}}},
	}
	if err := renderTemplate(schemafShTemplate, "schemaf.sh", data); err != nil {
		t.Fatal(err)
	}
	if err := renderTemplate(testEnvShTemplate, "gen/test-env.sh", data); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"schemaf.sh", "gen/test-env.sh"} {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		lines, sawFunc := outsideRuntimeFunc(string(b))
		if path == "schemaf.sh" && !sawFunc {
			t.Fatal("schemaf.sh defines no _schemaf_container function")
		}
		calls := 0
		for _, line := range lines {
			if containerCall.MatchString(line) {
				t.Errorf("%s: container call bypasses _schemaf_container: %q", path, line)
			}
			if strings.Contains(line, "_schemaf_container ") {
				calls++
			}
		}
		if calls == 0 {
			t.Errorf("%s: expected container calls through _schemaf_container, found none", path)
		}
	}
}

// TestRepoSchemafShUsesRuntimeFunction checks the repo's own schemaf.sh too.
func TestRepoSchemafShUsesRuntimeFunction(t *testing.T) {
	b, err := os.ReadFile("../../../schemaf.sh")
	if err != nil {
		t.Fatal(err)
	}
	lines, sawFunc := outsideRuntimeFunc(string(b))
	if !sawFunc {
		t.Fatal("schemaf.sh defines no _schemaf_container function")
	}
	for _, line := range lines {
		if containerCall.MatchString(line) {
			t.Errorf("schemaf.sh: container call bypasses _schemaf_container: %q", line)
		}
	}
}
