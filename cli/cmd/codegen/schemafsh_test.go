package codegen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	cli "github.com/flocko-motion/schemaf/cli"
)

// containerCall matches a container runtime CLI in command position — at the
// start of a command, after a separator, `then`/`do`/`exec`, or inside $( ) —
// either by name or as "$rt". Messages that merely name a runtime don't match.
var containerCall = regexp.MustCompile(`(^|[;&|({]|\bthen|\bdo|\bexec|\$\()\s*("?\$\{?rt\}?"?|docker|podman)(\s|$)`)

// heredocStart matches a line that opens a here-document; group 1 is its delimiter.
var heredocStart = regexp.MustCompile(`<<-?'?([A-Za-z_]+)'?\s*$`)

// plainString matches a one-line double-quoted string without a command
// substitution: text (e.g. a message naming a runtime), never a call.
var plainString = regexp.MustCompile(`"(?:[^"\\$]|\\.|\$[^(])*"`)

// outsideRuntimeFunc returns the script's code lines that lie outside the
// _schemaf_container function, with comments, here-document bodies and plain
// double-quoted strings stripped, and whether the function was defined.
func outsideRuntimeFunc(script string) (lines []string, sawFunc bool) {
	inFunc := false
	heredoc := ""
	for _, line := range strings.Split(script, "\n") {
		if heredoc != "" {
			if line == heredoc {
				heredoc = ""
			}
			continue
		}
		if m := heredocStart.FindStringSubmatch(line); m != nil {
			heredoc = m[1]
		}
		line = plainString.ReplaceAllString(line, `""`)
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
	for k, v := range volumeConflictShell("myapp") {
		data[k] = v
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

// renderSchemafSh renders schemaf.sh.tmpl for project myapp into dir and
// returns its text.
func renderSchemafSh(t *testing.T, dir string) string {
	t.Helper()
	data := map[string]any{
		"Name": "myapp", "Port": 8000, "FrontendPort": 8002, "PostgresPort": 8003,
		"SchemafVersion": "v1.0.0", "InputGlobs": "go/api/*.go", "InputExcludes": "go/api/*.gen.go",
	}
	for k, v := range volumeConflictShell("myapp") {
		data[k] = v
	}
	path := filepath.Join(dir, "schemaf.sh")
	if err := renderTemplate(schemafShTemplate, path, data); err != nil {
		t.Fatal(err)
	}
	return readFile(t, path)
}

// shellFunc returns the definition of a function in a rendered script: from
// its "name() {" line to the first line that is exactly "}".
func shellFunc(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, "\n"+name+"() {\n")
	if start < 0 {
		t.Fatalf("schemaf.sh defines no %s", name)
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("%s has no closing brace", name)
	}
	return script[start+1 : start+end+3]
}

// TestShellVolumeCheck runs _schemaf_check_volume against stub podman/docker
// CLIs, with the same matrix as the Go check (cli.TestVolumeCheck), and
// requires exactly the Go refusal text.
func TestShellVolumeCheck(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not installed")
	}
	script := renderSchemafSh(t, t.TempDir())
	funcs := shellFunc(t, script, "_schemaf_container") + shellFunc(t, script, "_schemaf_check_volume")

	type stub struct{ unusable, volume bool }
	writeStub := func(dir string, name cli.Runtime, s stub) {
		rc := func(fail bool) int {
			if fail {
				return 1
			}
			return 0
		}
		body := fmt.Sprintf("#!/bin/sh\ncase \"$1 $2\" in\n  \"info \"*) exit %d ;;\n  \"volume inspect\") exit %d ;;\nesac\nexit 0\n",
			rc(s.unusable), rc(!s.volume))
		if err := os.WriteFile(filepath.Join(dir, string(name)), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}

	for _, sel := range []cli.Runtime{cli.Podman, cli.Docker} {
		other := sel.Other()
		for _, tc := range []struct {
			name   string
			stubs  map[cli.Runtime]stub
			refuse bool
			both   bool
		}{
			{name: "neither has it", stubs: map[cli.Runtime]stub{sel: {}, other: {}}},
			{name: "only selected has it", stubs: map[cli.Runtime]stub{sel: {volume: true}, other: {}}},
			{name: "only other has it", stubs: map[cli.Runtime]stub{sel: {}, other: {volume: true}}, refuse: true},
			{name: "both have it", stubs: map[cli.Runtime]stub{sel: {volume: true}, other: {volume: true}}, refuse: true, both: true},
			{name: "other CLI absent", stubs: map[cli.Runtime]stub{sel: {}}},
			{name: "other CLI not usable", stubs: map[cli.Runtime]stub{sel: {}, other: {unusable: true, volume: true}}},
		} {
			t.Run(fmt.Sprintf("%s selected/%s", sel, tc.name), func(t *testing.T) {
				bin := t.TempDir()
				if err := os.Symlink(catPath, filepath.Join(bin, "cat")); err != nil {
					t.Fatal(err)
				}
				for name, s := range tc.stubs {
					writeStub(bin, name, s)
				}
				cmd := exec.Command(bash, "-c", "set -euo pipefail\nPROJECT_NAME=myapp\n"+funcs+"_schemaf_check_volume\necho STARTED")
				cmd.Env = []string{"PATH=" + bin, "SCHEMAF_RUNTIME=" + string(sel)}
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				if !tc.refuse {
					if err != nil || stdout.String() != "STARTED\n" {
						t.Fatalf("want start, got %v:\n%s", err, stderr.String())
					}
					return
				}
				if err == nil || strings.Contains(stdout.String(), "STARTED") {
					t.Fatalf("want refusal, got start:\n%s", stderr.String())
				}
				want := "ERROR: " + cli.VolumeConflictMessage(sel, "myapp", tc.both) + "\n"
				if got := stderr.String(); got != want {
					t.Errorf("shell refusal differs from Go's.\n--- shell\n%s\n--- go\n%s", got, want)
				}
			})
		}
	}
}
