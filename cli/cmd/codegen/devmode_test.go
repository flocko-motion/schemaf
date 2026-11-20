package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGo records the environment `go` is started with (schemaf.sh hands every
// non-build/run command to `go run`).
const fakeGo = `#!/bin/bash
env > "${FAKE_STATE:?}/go_env"
`

// TestSchemafShTurnsOnDevModeWithDevEnv: every command that loads the dev env
// file runs with SCHEMAF_DEV=1.
func TestSchemafShTurnsOnDevModeWithDevEnv(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	if err := os.WriteFile(filepath.Join(u.bin, "go"), []byte(fakeGo), 0755); err != nil {
		t.Fatal(err)
	}
	withEnvFile(t, u, "dev/etc/env", "DB_PASS=x\n")
	for _, args := range [][]string{{"dev", "all"}, {"auth", "token", "alice"}, {"import-data"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_ = os.Remove(filepath.Join(u.state, "go_env"))
			if _, stderr, code := u.run(root, "schemaf.sh", args...); code != 0 {
				t.Fatalf("schemaf.sh %s failed:\n%s", strings.Join(args, " "), stderr)
			}
			mustContain(t, "go environment", readFile(t, filepath.Join(u.state, "go_env")), "\nSCHEMAF_DEV=1\n")
		})
	}
}

// withDevShell returns a wrapper script that runs the project's schemaf.sh
// from a shell that exports SCHEMAF_DEV=1.
func withDevShell(t *testing.T) string {
	t.Helper()
	wrapper := filepath.Join(t.TempDir(), "with-dev.sh")
	if err := os.WriteFile(wrapper, []byte("export SCHEMAF_DEV=1\nexec bash \"$PWD/schemaf.sh\" \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

// TestSchemafShRunIsProduction: run starts production even when the caller's
// shell exports SCHEMAF_DEV=1.
func TestSchemafShRunIsProduction(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	if _, stderr, code := u.run(root, withDevShell(t), "run", "-d"); code != 0 {
		t.Fatalf("run failed:\n%s", stderr)
	}
	mustNotContain(t, "compose environment", readFile(t, filepath.Join(u.state, "compose_env")), "SCHEMAF_DEV=")
}

// TestSchemafShBuildIsProduction: build never passes SCHEMAF_DEV on, even when
// the caller's shell exports it.
func TestSchemafShBuildIsProduction(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	if _, stderr, code := u.run(root, withDevShell(t), "build"); code != 0 {
		t.Fatalf("build failed:\n%s", stderr)
	}
	mustNotContain(t, "build environment", readFile(t, filepath.Join(u.state, "build_env")), "SCHEMAF_DEV=")
}

// TestSchemafShRefusesDevModeInEnvFile: the mode belongs to schemaf.sh. An env
// file that sets SCHEMAF_DEV (to anything, even empty) stops every command
// that reads it, naming the variable and the file, before anything starts.
func TestSchemafShRefusesDevModeInEnvFile(t *testing.T) {
	root := newDeployProject(t)
	for _, tc := range []struct {
		file, line string
		args       []string
	}{
		{file: "etc/env", line: "SCHEMAF_DEV=1", args: []string{"run", "-d"}},
		{file: "etc/env", line: "  SCHEMAF_DEV = 1", args: []string{"run", "-d"}},
		{file: "dev/etc/env", line: "SCHEMAF_DEV=", args: []string{"auth", "token", "alice"}},
		{file: "dev/etc/env", line: "SCHEMAF_DEV=1", args: []string{"build"}},
	} {
		t.Run(tc.file+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			u := newShUser(t)
			if err := os.WriteFile(filepath.Join(u.bin, "go"), []byte(fakeGo), 0755); err != nil {
				t.Fatal(err)
			}
			withEnvFile(t, u, tc.file, "DB_PASS=x\n"+tc.line+"\n")
			_, stderr, code := u.run(root, "schemaf.sh", tc.args...)
			if code == 0 {
				t.Fatalf("schemaf.sh %s with %q in %s: exit 0, want failure", strings.Join(tc.args, " "), tc.line, tc.file)
			}
			mustContain(t, "stderr", stderr, "ERROR:", "SCHEMAF_DEV", filepath.Join(".myapp", tc.file))
			calls := u.calls()
			mustNotContain(t, "runtime calls", calls, "build", "compose")
			if _, err := os.Stat(filepath.Join(u.state, "go_env")); err == nil {
				t.Errorf("go was started despite the refusal")
			}
		})
	}
}

// TestComposePinsProductionMode: the generated backend service sets
// SCHEMAF_DEV empty in environment:, which beats a SCHEMAF_DEV=1 in env_file.
func TestComposePinsProductionMode(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	s := readFile(t, "compose.gen.yml")
	backend := s[strings.Index(s, "  backend:"):]
	backend = backend[:strings.Index(backend, "\n\n")]
	env := backend[strings.Index(backend, "    environment:\n"):]
	env = env[:strings.Index(env, "    ports:")]
	mustContain(t, "backend environment", env, `      SCHEMAF_DEV: ""`)
}
