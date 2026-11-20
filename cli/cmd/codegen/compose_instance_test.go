package codegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/flocko-motion/schemaf/cli"
)

// composeDevConfig returns `compose config` of the dev stack (compose.gen.yml +
// compose.dev.gen.yml) with the given extra environment.
func composeDevConfig(t *testing.T, env []string) map[string]any {
	t.Helper()
	cmd, err := cli.Container("compose", "-f", "compose.gen.yml", "-f", "compose.dev.gen.yml", "config", "--format", "json")
	if err != nil {
		t.Skipf("no container runtime: %v", err)
	}
	for _, k := range []string{"SCHEMAF_IMAGE", cli.InstanceSuffixEnv, cli.PostgresPortEnv} {
		cmd.Env = withoutEnv(cmd.Env, k)
	}
	cmd.Env = append(append(cmd.Env, "SCHEMAF_IMAGE=myapp:test"), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("compose not usable here: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("parsing compose config: %v\n%s", err, out)
	}
	return cfg
}

// get walks a decoded JSON document by keys (map keys or a slice index "0").
func get(t *testing.T, v any, keys ...string) string {
	t.Helper()
	for _, k := range keys {
		switch m := v.(type) {
		case map[string]any:
			v = m[k]
		case []any:
			if len(m) == 0 {
				t.Fatalf("empty list at %q", k)
			}
			v = m[0]
		default:
			t.Fatalf("no %q in %v", strings.Join(keys, "."), v)
		}
	}
	s, _ := v.(string)
	return s
}

// The generated dev compose takes the instance's names and Postgres port from
// the environment; without it, every name and port is the default instance's
// (INSTALL.md › Parallel dev instances).
func TestComposeInstanceNames(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		env       []string
		stack, pg string
	}{
		{"no instance env", nil, "myapp", "8003"},
		{"default instance", cli.InstanceComposeEnv(0, 8003), "myapp", "8003"},
		{"instance 349", cli.InstanceComposeEnv(349, 11493), "myapp-349", "11493"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := composeDevConfig(t, tc.env)
			for what, got := range map[string]string{
				"compose project": get(t, cfg, "name"),
				"postgres":        get(t, cfg, "services", "postgres", "container_name"),
				"backend":         get(t, cfg, "services", "backend", "container_name"),
				"network":         get(t, cfg, "networks", "net", "name"),
				"volume":          get(t, cfg, "volumes", "postgres-data", "name"),
				"postgres port":   get(t, cfg, "services", "postgres", "ports", "0", "published"),
				"database name":   get(t, cfg, "services", "postgres", "environment", "POSTGRES_DB"),
			} {
				want := map[string]string{
					"compose project": tc.stack,
					"postgres":        tc.stack + "-postgres",
					"backend":         tc.stack + "-backend",
					"network":         tc.stack + "-net",
					"volume":          tc.stack + "-postgres-data",
					"postgres port":   tc.pg,
					"database name":   "myapp", // the same in every instance
				}[what]
				if got != want {
					t.Errorf("%s: %q, want %q", what, got, want)
				}
			}
		})
	}
}

// Only `dev` (in Go) names an instance. An instance variable from an env file
// never reaches production compose, so `run` always starts the default stack.
func TestRunIgnoresInstanceVariables(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	envDir := filepath.Join(u.home, ".myapp", "etc")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "env"),
		[]byte(cli.InstanceSuffixEnv+"=-7\n"+cli.PostgresPortEnv+"=9999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := u.run(root, "schemaf.sh", "run", "-d"); code != 0 {
		t.Fatalf("run failed:\n%s", stderr)
	}
	env := readFile(t, filepath.Join(u.state, "compose_env"))
	mustNotContain(t, "compose env", env, cli.InstanceSuffixEnv+"=", cli.PostgresPortEnv+"=")
}

// --instance is dev-only: build and run refuse it with that reason, before
// building or starting anything.
func TestBuildAndRunRejectInstance(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	for _, args := range [][]string{{"run", "--instance", "3"}, {"run", "-d", "--instance=3"}, {"build", "--instance", "3"}} {
		_, stderr, code := u.run(root, "schemaf.sh", args...)
		if code == 0 || !strings.Contains(stderr, "--instance is dev-only") {
			t.Errorf("%v: want dev-only refusal, got exit %d:\n%s", args, code, stderr)
		}
		mustNotContain(t, "calls", u.calls(), "build", "compose")
	}
}
