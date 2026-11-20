package codegen

// Tests for the build / artifact / run-side logic in the generated schemaf.sh
// (INSTALL.md › Deployment). They run the rendered script with bash against a
// fake container runtime, so no podman or Docker is needed.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	cli "github.com/flocko-motion/schemaf/cli"
)

// writeMinimalProject writes the smallest project codegen accepts into the
// current directory.
func writeMinimalProject(t *testing.T) {
	t.Helper()
	files := map[string]string{
		"schemaf.toml":                   "title = \"My App\"\nname = \"myapp\"\n",
		"go/go.mod":                      "module myapp\n\ngo 1.24\n\nrequire github.com/flocko-motion/schemaf v1.11.0\n",
		"go/api/hello.go":                "package api\n",
		"go/db/migrations/0001_init.sql": "CREATE TABLE t (id int);\n",
		"go/db/queries/t.sql":            "-- name: ListT :many\nSELECT * FROM t;\n",
		"compose/redis.yml":              "services:\n  redis:\n    image: redis\n",
		"compose/tools.dev.yml":          "services:\n  tools:\n    image: busybox\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeRuntime is a stand-in for podman and docker. State lives in $FAKE_STATE;
// every call is appended to $FAKE_STATE/calls. docker is "installed but not
// usable" unless $FAKE_STATE/docker_usable exists, so the volume check skips it.
const fakeRuntime = `#!/bin/bash
set -u
me=$(basename "$0")
S="${FAKE_STATE:?}"
mkdir -p "$S/images"
echo "$me $* | SCHEMAF_IMAGE=${SCHEMAF_IMAGE:-}" >> "$S/calls"
if [ "$me" = docker ] && [ "${1:-}" != --help ] && [ ! -f "$S/docker_usable" ]; then echo "docker: daemon not running" >&2; exit 1; fi
key() { echo "$1" | sed 's#^localhost/##; s#[:/]#_#g'; }
case "${1:-}" in
  --help|info) exit 0 ;;
  compose)
    case "$*" in *" version"*) echo "Docker Compose version v2.30.0" ;; esac
    env > "$S/compose_env"; exit 0 ;;
  build)
    shift; img=""; labels=""
    while [ $# -gt 1 ]; do
      case "$1" in
        -t) img="$2"; shift 2 ;;
        --label) labels="$labels$2"$'\n'; shift 2 ;;
        -f) shift 2 ;;
        *) shift ;;
      esac
    done
    d="$S/images/$(key "$img")"; mkdir -p "$d"
    printf '%s' "$labels" > "$d/labels"; echo "$FAKE_PLATFORM" > "$d/platform"; echo "$img" > "$d/name"
    echo "$1" > "$S/last_ctx"; env > "$S/build_env"; exit 0 ;;
  image)
    sub="$2"; shift 2
    case "$sub" in
      inspect)
        fmt=""; if [ "$1" = --format ]; then fmt="$2"; shift 2; fi
        d="$S/images/$(key "$1")"; [ -d "$d" ] || { echo "Error: no such image $1" >&2; exit 1; }
        case "$fmt" in
          "") echo "[{}]" ;;
          *Labels*) l=$(echo "$fmt" | sed 's/.*"\(.*\)".*/\1/'); grep "^$l=" "$d/labels" | head -n 1 | cut -d= -f2- ;;
          *Architecture*) cat "$d/platform" ;;
          *Size*) echo 123456789 ;;
        esac
        exit 0 ;;
      rm) rm -rf "$S/images/$(key "$1")"; exit 0 ;;
    esac ;;
  save)
    d="$S/images/$(key "$4")"
    { echo "IMAGE $(cat "$d/name")"; cat "$d/platform"; cat "$d/labels"; } > "$3"; exit 0 ;;
  load)
    img=$(head -n 1 "$3" | cut -d' ' -f2); d="$S/images/$(key "$img")"; mkdir -p "$d"
    echo "$img" > "$d/name"; sed -n 2p "$3" > "$d/platform"; tail -n +3 "$3" > "$d/labels"
    echo "Loaded image: $img"; exit 0 ;;
  container)
    [ -f "$S/container" ] || { echo "Error: no such container" >&2; exit 1; }
    cat "$S/container"; exit 0 ;;
  volume) exit 1 ;;
  exec)
    # $S/exec_fail holds "<exit code> <message>": the app binary fails with it.
    if [ -f "$S/exec_fail" ]; then read -r rc msg < "$S/exec_fail"; echo "$msg" >&2; exit "$rc"; fi
    exit 0 ;;
esac
echo "fake runtime: unhandled call: $*" >&2
exit 1
`

// shUser is one OS user: a HOME and a container store (fake runtime state).
type shUser struct {
	t     *testing.T
	home  string
	state string
	bin   string
}

func newShUser(t *testing.T) *shUser {
	t.Helper()
	u := &shUser{t: t, home: t.TempDir(), state: t.TempDir(), bin: t.TempDir()}
	for _, name := range []string{"podman", "docker"} {
		if err := os.WriteFile(filepath.Join(u.bin, name), []byte(fakeRuntime), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

// run executes a script with bash as this user and returns stdout, stderr and
// the exit code.
func (u *shUser) run(dir, script string, args ...string) (string, string, int) {
	u.t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + u.bin + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + u.home,
		"TMPDIR=" + u.t.TempDir(),
		"FAKE_STATE=" + u.state,
		"FAKE_PLATFORM=linux/" + runtime.GOARCH,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		u.t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// calls returns the fake runtime's call log and clears it.
func (u *shUser) calls() string {
	u.t.Helper()
	p := filepath.Join(u.state, "calls")
	b, _ := os.ReadFile(p)
	_ = os.Remove(p)
	return string(b)
}

func (u *shUser) setContainer(content string) {
	u.t.Helper()
	p := filepath.Join(u.state, "container")
	if content == "" {
		_ = os.Remove(p)
		return
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		u.t.Fatal(err)
	}
}

// newDeployProject generates a project in a temp dir (codegen compose + inputs
// hash), commits it, and returns its root. It changes the working directory.
func newDeployProject(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"bash", "git", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	root := t.TempDir()
	t.Chdir(root)
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	if err := runInputsHashGen(); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	commitAll(t, root, "init")
	return root
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitAll(t *testing.T, dir, msg string) string {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "--short", "HEAD")
}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, got)
		}
	}
}

func mustNotContain(t *testing.T, what, got string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(got, w) {
			t.Errorf("%s contains %q:\n%s", what, w, got)
		}
	}
}

// extractInputsHash returns the inputs-hash section of a rendered schemaf.sh.
func extractInputsHash(t *testing.T, script string) string {
	t.Helper()
	start := strings.Index(script, "# >>> inputs-hash")
	end := strings.Index(script, "# <<< inputs-hash")
	if start < 0 || end < start {
		t.Fatal("schemaf.sh has no inputs-hash section")
	}
	return script[start:end]
}

func TestInputsHashShellAgreesWithGo(t *testing.T) {
	root := newDeployProject(t)
	funcs := extractInputsHash(t, readFile(t, "schemaf.sh"))
	shellHash := func() string {
		t.Helper()
		cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+funcs+"\n_schemaf_inputs_hash")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("shell inputs hash: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	goHash := func() string {
		t.Helper()
		h, err := inputsHash()
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	if g, s := goHash(), shellHash(); g != s {
		t.Fatalf("Go and shell disagree: go %s, shell %s", g, s)
	}
	if got := strings.TrimSpace(readFile(t, inputsHashFile)); got != goHash() {
		t.Fatalf("%s = %s, want %s", inputsHashFile, got, goHash())
	}

	// Non-inputs change neither hash: generated Go, dotfiles, dev-only paths
	// outside the globs, files in subdirectories.
	before := goHash()
	for path, content := range map[string]string{
		"go/api/endpoints.gen.go":       "package api\n",
		"go/db/migrations/.DS_Store":    "x",
		"go/db/migrations/sub/0002.sql": "x",
		"compose/notes.txt":             "x",
		"README.md":                     "x",
	} {
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if g, s := goHash(), shellHash(); g != before || s != before {
		t.Fatalf("non-inputs changed the hash: before %s, go %s, shell %s", before, g, s)
	}

	// Each kind of input changes both hashes the same way.
	for _, change := range []struct{ path, content string }{
		{"go/api/hello.go", "package api\n// changed\n"},
		{"go/api/new.go", "package api\n"},
		{"go/db/migrations/0002_more.sql", "SELECT 1;\n"},
		{"go/db/queries/t.sql", "-- changed\n"},
		{"schemaf.toml", "title = \"Other\"\nname = \"myapp\"\n"},
		{"compose/tools.dev.yml", "services: {}\n"},
		{"go/go.mod", "module myapp\n\ngo 1.24\n\nrequire github.com/flocko-motion/schemaf v1.12.0\n"},
	} {
		prev := goHash()
		if err := os.WriteFile(change.path, []byte(change.content), 0644); err != nil {
			t.Fatal(err)
		}
		g, s := goHash(), shellHash()
		if g == prev {
			t.Errorf("changing %s did not change the hash", change.path)
		}
		if g != s {
			t.Errorf("after changing %s, Go and shell disagree: go %s, shell %s", change.path, g, s)
		}
	}

	// A package is an input once gen/schema-sources lists it; its generated
	// and test files and dotfiles never are. A glob listed twice, or also
	// covered by inputGlobs, counts once.
	write := func(path, content string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	agree := func(what string, wantChange bool) {
		t.Helper()
		prev := before
		g, s := goHash(), shellHash()
		if g != s {
			t.Errorf("%s: Go and shell disagree: go %s, shell %s", what, g, s)
		}
		if (g != prev) != wantChange {
			t.Errorf("%s: hash changed = %v, want %v", what, g != prev, wantChange)
		}
		before = g
	}
	before = goHash()
	write("go/obj/user.go", "package obj\n")
	agree("unlisted package", false)
	write(schemaSourcesFile, "go/obj/*.go\ngo/api/*.go\ngo/obj/*.go\n")
	agree("listing go/obj", true)
	write("go/obj/user.go", "package obj\n// changed\n")
	agree("changing a listed package", true)
	write("go/obj/user_test.go", "package obj\n")
	write("go/obj/user.gen.go", "package obj\n")
	write("go/obj/.hidden.go", "package obj\n")
	agree("generated, test and dot files in a listed package", false)
	write("go/obj/address.go", "package obj\n")
	agree("a new file in a listed package", true)
	write(schemaSourcesFile, "go/obj/*.go\n")
	agree("dropping a duplicate glob", false)
}

// TestInputsHashShasumFallback hides sha256sum, so the shell hash takes the
// shasum -a 256 path (macOS), and checks it still equals Go's.
func TestInputsHashShasumFallback(t *testing.T) {
	root := newDeployProject(t)
	shasum, err := exec.LookPath("shasum")
	if err != nil {
		t.Skip("shasum not installed")
	}
	bash, _ := exec.LookPath("bash")
	// PATH holds exactly the tools the inputs hash needs, without sha256sum.
	bin := t.TempDir()
	for _, tool := range []string{"awk", "sort", "cut", "perl"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			if tool == "perl" {
				continue // shasum may not need perl on PATH (#!/usr/bin/perl)
			}
			t.Skipf("%s not installed", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(shasum, filepath.Join(bin, "shasum")); err != nil {
		t.Fatal(err)
	}

	funcs := extractInputsHash(t, readFile(t, "schemaf.sh"))
	cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+
		"! command -v sha256sum >/dev/null || { echo 'sha256sum still on PATH' >&2; exit 9; }\n"+
		funcs+"\n_schemaf_inputs_hash")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + bin}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shell inputs hash via shasum: %v\n%s", err, out)
	}
	want, err := inputsHash()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("shasum path: shell %s, go %s", got, want)
	}
}

func TestReadSchemafVersion(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.Mkdir("go", 0755)
	for _, tc := range []struct{ gomod, want string }{
		{"module x\nrequire github.com/flocko-motion/schemaf v1.2.3\n", "v1.2.3"},
		{"module x\nrequire (\n\tgithub.com/spf13/cobra v1.8.0\n\tgithub.com/flocko-motion/schemaf v1.4.0 // indirect\n)\n", "v1.4.0"},
		{"module x\n// github.com/flocko-motion/schemaf v0.0.1\nrequire github.com/flocko-motion/schemaf v2.0.0\nreplace github.com/flocko-motion/schemaf => ../schemaf\n", "v2.0.0"},
		{"module x\n", ""},
	} {
		if err := os.WriteFile("go/go.mod", []byte(tc.gomod), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := readSchemafVersion()
		if tc.want == "" {
			if err == nil {
				t.Errorf("%q: want an error, got %q", tc.gomod, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.gomod, got, err, tc.want)
		}
	}
}

func TestBuild(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	short := git(t, root, "rev-parse", "--short", "HEAD")

	t.Run("clean build uses the commit, from git archive, labelled", func(t *testing.T) {
		_, stderr, code := u.run(root, "schemaf.sh", "build")
		if code != 0 {
			t.Fatalf("build failed (%d):\n%s", code, stderr)
		}
		calls := u.calls()
		mustContain(t, "build calls", calls, "podman build", "-t myapp:"+short, "--label schemaf.dirty=false", "--label schemaf.commit=")
		ctx, _ := filepath.EvalSymlinks(strings.TrimSpace(readFile(t, filepath.Join(u.state, "last_ctx"))))
		if want, _ := filepath.EvalSymlinks(root); ctx == want {
			t.Error("a clean build used the working tree, not git archive")
		}
	})

	t.Run("already built commit is skipped unless --force", func(t *testing.T) {
		_, stderr, code := u.run(root, "schemaf.sh", "build")
		if code != 0 {
			t.Fatalf("build failed:\n%s", stderr)
		}
		mustContain(t, "stderr", stderr, "Already built: myapp:"+short)
		mustNotContain(t, "calls", u.calls(), "podman build")

		if _, stderr, code := u.run(root, "schemaf.sh", "build", "--force"); code != 0 {
			t.Fatalf("build --force failed:\n%s", stderr)
		}
		mustContain(t, "calls", u.calls(), "podman build")
	})

	t.Run("build reads only SCHEMAF_RUNTIME from the env file", func(t *testing.T) {
		withEnvFile(t, u, "dev/etc/env", "SCHEMAF_RUNTIME=docker\nDB_PASS=x\nPUBLIC_FOO=y\n")
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--force")
		if code != 0 {
			t.Fatalf("build failed:\n%s", stderr)
		}
		calls := u.calls()
		mustContain(t, "calls", calls, "docker build")
		mustNotContain(t, "calls", calls, "podman build", "DB_PASS", "PUBLIC_FOO")
		mustNotContain(t, "build environment", readFile(t, filepath.Join(u.state, "build_env")), "DB_PASS", "PUBLIC_FOO")
	})

	t.Run("uncommitted changes: plain build always rebuilds, tagged -dirty", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(filepath.Join(root, "scratch.txt"))
		for i := 0; i < 2; i++ {
			if _, stderr, code := u.run(root, "schemaf.sh", "build"); code != 0 {
				t.Fatalf("dirty build failed:\n%s", stderr)
			}
			calls := u.calls()
			mustContain(t, "calls", calls, "podman build", "-t myapp:"+short+"-dirty", "--label schemaf.dirty=true")
			ctx, _ := filepath.EvalSymlinks(strings.TrimSpace(readFile(t, filepath.Join(u.state, "last_ctx"))))
			if want, _ := filepath.EvalSymlinks(root); ctx != want {
				t.Errorf("dirty build context = %s, want the working tree %s", ctx, want)
			}
		}
	})

	t.Run("build --output refuses uncommitted changes", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(filepath.Join(root, "scratch.txt"))
		out := filepath.Join(t.TempDir(), "a.run")
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--output", out)
		if code == 0 {
			t.Fatal("build --output accepted uncommitted changes")
		}
		mustContain(t, "stderr", stderr, "commit first — a deployment artifact must name exactly one commit")
		if _, err := os.Stat(out); err == nil {
			t.Error("artifact written despite the refusal")
		}
	})

	t.Run("build --output refuses an existing file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "a.run")
		if err := os.WriteFile(out, []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--output", out)
		if code == 0 {
			t.Fatal("build --output overwrote an existing file")
		}
		mustContain(t, "stderr", stderr, "already exists")
		if readFile(t, out) != "keep" {
			t.Error("existing file was changed")
		}
	})

	t.Run("stale generated files are refused", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "go/api/hello.go"), []byte("package api\n// new endpoint\n"), 0644); err != nil {
			t.Fatal(err)
		}
		commitAll(t, root, "forgot codegen")
		defer func() {
			git(t, root, "reset", "-q", "--hard", "HEAD~1")
		}()
		for _, args := range [][]string{{"build"}, {"test"}} {
			_, stderr, code := u.run(root, "schemaf.sh", args...)
			if code == 0 {
				t.Fatalf("%v accepted stale generated files", args)
			}
			mustContain(t, "stderr", stderr, "generated files are stale — run ./schemaf.sh codegen and commit")
		}
	})

	t.Run("empty inputs are printed as warnings, never stop the build", func(t *testing.T) {
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--force")
		if code != 0 {
			t.Fatalf("build failed:\n%s", stderr)
		}
		mustNotContain(t, "stderr with no empty inputs", stderr, "⚠")

		warnings := "no migrations in go/db/migrations — db.Provider() is empty\n" +
			"no endpoints in go/api — Provider() registers nothing\n"
		if err := os.WriteFile(filepath.Join(root, emptyInputsFile), []byte(warnings), 0644); err != nil {
			t.Fatal(err)
		}
		commitAll(t, root, "empty inputs")
		defer func() {
			git(t, root, "reset", "-q", "--hard", "HEAD~1")
		}()
		_, stderr, code = u.run(root, "schemaf.sh", "build")
		if code != 0 {
			t.Fatalf("build with empty inputs failed:\n%s", stderr)
		}
		mustContain(t, "stderr", stderr,
			"⚠ no migrations in go/db/migrations — db.Provider() is empty\n⚠ no endpoints in go/api — Provider() registers nothing\n")
		if strings.Index(stderr, "⚠") > strings.Index(stderr, "Building ") {
			t.Errorf("warnings printed after the build started:\n%s", stderr)
		}
	})

	t.Run("invalid tag", func(t *testing.T) {
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--tag", "-bad/tag")
		if code == 0 {
			t.Fatal("accepted an invalid tag")
		}
		mustContain(t, "stderr", stderr, "invalid tag")
	})
}

// buildArtifact builds the project's HEAD into an artifact with the given tag.
func buildArtifact(t *testing.T, u *shUser, root, tag string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "myapp-"+tag+".run")
	_, stderr, code := u.run(root, "schemaf.sh", "build", "--tag", tag, "--output", out)
	if code != 0 {
		t.Fatalf("build --output failed:\n%s", stderr)
	}
	u.calls()
	return out
}

func TestArtifactLoadAndRunSide(t *testing.T) {
	root := newDeployProject(t)
	builder := newShUser(t)
	runner := newShUser(t) // a second user: own HOME, own container store
	runSide := filepath.Join(runner.home, ".myapp", "bin", "schemaf.sh")

	art1 := buildArtifact(t, builder, root, "v1")
	commit1 := git(t, root, "rev-parse", "HEAD")

	t.Run("artifact is executable and needs the load argument", func(t *testing.T) {
		info, err := os.Stat(art1)
		if err != nil || info.Mode()&0111 == 0 {
			t.Fatalf("artifact not executable: %v %v", info, err)
		}
		_, stderr, code := runner.run(runner.home, art1)
		if code == 0 {
			t.Fatal("artifact without arguments succeeded")
		}
		mustContain(t, "stderr", stderr, "usage:", "load")
		mustNotContain(t, "artifact header", readFile(t, art1)[:600], "DB_PASS")
	})

	t.Run("load: image into the user's store, deploy dir, run-side script", func(t *testing.T) {
		stdout, stderr, code := runner.run(runner.home, art1, "load")
		if code != 0 {
			t.Fatalf("load failed:\n%s", stderr)
		}
		if strings.TrimSpace(stdout) != "v1" {
			t.Errorf("load printed %q, want the tag v1", stdout)
		}
		mustContain(t, "calls", runner.calls(), "podman load -i")
		deploy := filepath.Join(runner.home, ".myapp", "deploy", "v1")
		for _, f := range []string{"compose.gen.yml", "compose/redis.yml", "manifest"} {
			if _, err := os.Stat(filepath.Join(deploy, f)); err != nil {
				t.Errorf("deploy dir lacks %s", f)
			}
		}
		if _, err := os.Stat(filepath.Join(deploy, "compose/tools.dev.yml")); err == nil {
			t.Error("deploy dir contains a dev-only compose file")
		}
		mustContain(t, "manifest", readFile(t, filepath.Join(deploy, "manifest")),
			"name=myapp", "tag=v1", "commit="+commit1, "schemaf_version=v1.11.0", "platform=linux/"+runtime.GOARCH, "describe=")
		if info, err := os.Stat(runSide); err != nil || info.Mode()&0111 == 0 {
			t.Fatalf("run-side schemaf.sh not installed: %v", err)
		}
	})

	t.Run("load again, same commit: no-op", func(t *testing.T) {
		_, stderr, code := runner.run(runner.home, art1, "load")
		if code != 0 {
			t.Fatalf("second load failed:\n%s", stderr)
		}
		mustContain(t, "stderr", stderr, "v1 is already loaded")
		mustNotContain(t, "calls", runner.calls(), "podman load")
	})

	// A second commit, built as v2 and, to test reuse, as v1 again.
	if err := os.WriteFile(filepath.Join(root, "frontend.txt"), []byte("change"), 0644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "second")
	commit2 := git(t, root, "rev-parse", "HEAD")
	art2 := buildArtifact(t, builder, root, "v2")
	art1b := buildArtifact(t, builder, root, "v1")

	t.Run("same tag, different commit: refused", func(t *testing.T) {
		_, stderr, code := runner.run(runner.home, art1b, "load")
		if code == 0 {
			t.Fatal("reloading v1 from another commit succeeded")
		}
		mustContain(t, "stderr", stderr,
			"v1 is already loaded from commit "+commit1[:7]+", this archive is commit "+commit2[:7]+
				" — remove it first (schemaf.sh image rm v1) or build with a different tag")
	})

	t.Run("platform mismatch: refused", func(t *testing.T) {
		other := newShUser(t)
		out := filepath.Join(t.TempDir(), "x.run")
		cmd := exec.Command("bash", "schemaf.sh", "build", "--tag", "foreign", "--output", out)
		cmd.Dir = root
		cmd.Env = []string{"PATH=" + other.bin + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + other.home,
			"FAKE_STATE=" + other.state, "FAKE_PLATFORM=linux/s390x", "TMPDIR=" + t.TempDir()}
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, b)
		}
		_, stderr, code := runner.run(runner.home, out, "load")
		if code == 0 {
			t.Fatal("loaded an image for another platform")
		}
		mustContain(t, "stderr", stderr, "this archive's image is for linux/s390x")
	})

	if _, stderr, code := runner.run(runner.home, art2, "load"); code != 0 {
		t.Fatalf("load v2:\n%s", stderr)
	}
	runner.calls()

	t.Run("run --image starts from the deploy dir with SCHEMAF_IMAGE", func(t *testing.T) {
		_, stderr, code := runner.run(runner.home, runSide, "run", "--image", "v2", "-d")
		if code != 0 {
			t.Fatalf("run --image failed:\n%s", stderr)
		}
		deploy := filepath.Join(runner.home, ".myapp", "deploy", "v2", "compose.gen.yml")
		calls := runner.calls()
		mustContain(t, "calls", calls, "podman compose -f "+deploy+" up -d | SCHEMAF_IMAGE=myapp:v2")
		mustNotContain(t, "calls", calls, "podman build")
	})

	t.Run("run --image with a tag that isn't loaded", func(t *testing.T) {
		_, stderr, code := runner.run(runner.home, runSide, "run", "--image", "v9")
		if code == 0 {
			t.Fatal("started a tag that isn't loaded")
		}
		mustContain(t, "stderr", stderr, "myapp:v9 is not loaded")
		mustNotContain(t, "calls", runner.calls(), "compose -f")
	})

	t.Run("dev-host commands fail loudly", func(t *testing.T) {
		for _, c := range []string{"codegen", "test", "dev", "build", "upgrade"} {
			_, stderr, code := runner.run(runner.home, runSide, c)
			if code == 0 {
				t.Errorf("%s succeeded on the run side", c)
			}
			mustContain(t, c+" stderr", stderr, "this host has no checkout; that command runs on a dev or build host")
		}
	})

	t.Run("app subcommand needs a running backend", func(t *testing.T) {
		runner.setContainer("")
		for _, args := range [][]string{{"db", "backup"}, {"frobnicate"}} {
			_, stderr, code := runner.run(runner.home, runSide, args...)
			if code == 0 {
				t.Fatalf("%v ran without a backend", args)
			}
			mustContain(t, "stderr", stderr, "start it first: schemaf.sh run --image <tag>")
			mustNotContain(t, "calls", runner.calls(), " exec ")
		}
	})

	runner.setContainer("true localhost/myapp:v2")

	t.Run("app subcommand runs in the backend container", func(t *testing.T) {
		runner.calls()
		_, stderr, code := runner.run(runner.home, runSide, "db", "backup", "--local", "/tmp/x.sql.gz")
		if code != 0 {
			t.Fatalf("app subcommand failed:\n%s", stderr)
		}
		mustContain(t, "calls", runner.calls(), "podman exec -i myapp-backend /app db backup --local /tmp/x.sql.gz")
	})

	t.Run("unknown command goes to the app binary, its error and exit code come back", func(t *testing.T) {
		runner.calls()
		failFile := filepath.Join(runner.state, "exec_fail")
		if err := os.WriteFile(failFile, []byte("7 Error: unknown command \"frobnicate\" for \"myapp\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(failFile)
		_, stderr, code := runner.run(runner.home, runSide, "frobnicate", "--x")
		if code != 7 {
			t.Errorf("exit code %d, want the app's 7", code)
		}
		mustContain(t, "stderr", stderr, `Error: unknown command "frobnicate" for "myapp"`)
		mustContain(t, "calls", runner.calls(), "podman exec -i myapp-backend /app frobnicate --x")
	})

	t.Run("image ls marks the running tag", func(t *testing.T) {
		stdout, stderr, code := runner.run(runner.home, runSide, "image", "ls")
		if code != 0 {
			t.Fatalf("image ls failed:\n%s", stderr)
		}
		var v1, v2 string
		for _, line := range strings.Split(stdout, "\n") {
			switch {
			case strings.HasPrefix(line, "v1 "):
				v1 = line
			case strings.HasPrefix(line, "v2 "):
				v2 = line
			}
		}
		if !strings.Contains(v1, commit1[:7]) || strings.HasSuffix(v1, "*") {
			t.Errorf("v1 line: %q", v1)
		}
		if !strings.Contains(v2, commit2[:7]) || !strings.HasSuffix(v2, "*") || !strings.Contains(v2, "118MB") {
			t.Errorf("v2 line: %q", v2)
		}
	})

	t.Run("image rm refuses the running tag", func(t *testing.T) {
		_, stderr, code := runner.run(runner.home, runSide, "image", "rm", "v2")
		if code == 0 {
			t.Fatal("removed the running tag")
		}
		mustContain(t, "stderr", stderr, "v2 is running")
		if _, err := os.Stat(filepath.Join(runner.home, ".myapp", "deploy", "v2")); err != nil {
			t.Error("deploy dir of the running tag was removed")
		}
	})

	t.Run("stop and logs use the running deployment", func(t *testing.T) {
		runner.calls()
		if _, stderr, code := runner.run(runner.home, runSide, "logs", "-f", "backend"); code != 0 {
			t.Fatalf("logs failed:\n%s", stderr)
		}
		mustContain(t, "calls", runner.calls(), "deploy/v2/compose.gen.yml logs -f backend")
		if _, stderr, code := runner.run(runner.home, runSide, "stop"); code != 0 {
			t.Fatalf("stop failed:\n%s", stderr)
		}
		mustContain(t, "calls", runner.calls(), "deploy/v2/compose.gen.yml down")
	})

	t.Run("image rm removes another tag", func(t *testing.T) {
		if _, stderr, code := runner.run(runner.home, runSide, "image", "rm", "v1"); code != 0 {
			t.Fatalf("image rm v1 failed:\n%s", stderr)
		}
		mustContain(t, "calls", runner.calls(), "podman image rm myapp:v1")
		if _, err := os.Stat(filepath.Join(runner.home, ".myapp", "deploy", "v1")); err == nil {
			t.Error("deploy dir of v1 still exists")
		}
		// Now the v1 tag is free for the other commit.
		if _, stderr, code := runner.run(runner.home, art1b, "load"); code != 0 {
			t.Fatalf("loading v1 from the new commit after image rm failed:\n%s", stderr)
		}
	})

	t.Run("checkout: run --image points to the run side", func(t *testing.T) {
		_, stderr, code := builder.run(root, "schemaf.sh", "run", "--image", "v1")
		if code == 0 {
			t.Fatal("checkout run --image succeeded")
		}
		mustContain(t, "stderr", stderr, "~/.myapp/bin/schemaf.sh run --image <tag>")
	})
}

// TestArtifactComposeFromCommit: the artifact's compose files come from the
// commit, so a gitignored compose file (which doesn't make the tree dirty)
// never ships, even when codegen ran with it present.
func TestArtifactComposeFromCommit(t *testing.T) {
	root := newDeployProject(t)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("compose/extra.yml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "compose/extra.yml"), []byte("services:\n  extra:\n    image: busybox\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	if err := runInputsHashGen(); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "ignore extra")
	if out := git(t, root, "status", "--porcelain"); out != "" {
		t.Fatalf("tree should be clean, git status says:\n%s", out)
	}
	builder, runner := newShUser(t), newShUser(t)
	art := buildArtifact(t, builder, root, "v1")
	if _, stderr, code := runner.run(runner.home, art, "load"); code != 0 {
		t.Fatalf("load failed:\n%s", stderr)
	}
	deploy := filepath.Join(runner.home, ".myapp", "deploy", "v1")
	if _, err := os.Stat(filepath.Join(deploy, "compose/extra.yml")); err == nil {
		t.Error("a gitignored compose file shipped in the artifact")
	}
	if _, err := os.Stat(filepath.Join(deploy, "compose/redis.yml")); err != nil {
		t.Error("a committed compose file is missing from the artifact")
	}
}

// setSchemafVersion makes the project require schemaf version v, regenerates
// (schemaf.sh carries the version) and commits. The working directory must be root.
func setSchemafVersion(t *testing.T, root, v string) {
	t.Helper()
	gomod := "module myapp\n\ngo 1.24\n\nrequire github.com/flocko-motion/schemaf " + v + "\n"
	if err := os.WriteFile(filepath.Join(root, "go/go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	if err := runInputsHashGen(); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "schemaf "+v)
}

// TestLoadNeverDowngradesRunSideScript: load replaces ~/.<name>/bin/schemaf.sh
// only with an equal or newer schemaf version; the newer script runs the
// deploy dirs of older artifacts.
func TestLoadNeverDowngradesRunSideScript(t *testing.T) {
	root := newDeployProject(t)
	builder, runner := newShUser(t), newShUser(t)
	runSide := filepath.Join(runner.home, ".myapp", "bin", "schemaf.sh")
	load := func(t *testing.T, art string) string {
		t.Helper()
		_, stderr, code := runner.run(runner.home, art, "load")
		if code != 0 {
			t.Fatalf("load %s failed:\n%s", filepath.Base(art), stderr)
		}
		return stderr
	}
	installed := func(t *testing.T) string {
		t.Helper()
		return readFile(t, runSide)
	}

	setSchemafVersion(t, root, "v1.9.0")
	oldArt := buildArtifact(t, builder, root, "old")
	setSchemafVersion(t, root, "v1.12.0")
	newArt := buildArtifact(t, builder, root, "new")
	if err := os.WriteFile(filepath.Join(root, "frontend.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "same version, other commit")
	sameArt := buildArtifact(t, builder, root, "same")
	setSchemafVersion(t, root, "v1.9.0")
	old2Art := buildArtifact(t, builder, root, "old2")

	t.Run("first load installs the script", func(t *testing.T) {
		load(t, oldArt)
		mustContain(t, "installed script", installed(t), `SCHEMAF_VER="v1.9.0"`)
	})

	t.Run("newer artifact replaces it", func(t *testing.T) {
		stderr := load(t, newArt)
		mustContain(t, "installed script", installed(t), `SCHEMAF_VER="v1.12.0"`)
		mustNotContain(t, "stderr", stderr, "keeping run-side schemaf.sh")
	})

	t.Run("equal version replaces it", func(t *testing.T) {
		if err := os.WriteFile(runSide, []byte(installed(t)+"# test-marker-49\n"), 0755); err != nil {
			t.Fatal(err)
		}
		load(t, sameArt)
		mustNotContain(t, "installed script", installed(t), "# test-marker-49")
		mustContain(t, "installed script", installed(t), `SCHEMAF_VER="v1.12.0"`)
	})

	t.Run("older artifact keeps it, with one line saying so", func(t *testing.T) {
		before := installed(t)
		stderr := load(t, old2Art)
		mustContain(t, "stderr", stderr, "keeping run-side schemaf.sh v1.12.0 (artifact has v1.9.0)\n")
		if installed(t) != before {
			t.Error("an older artifact replaced the run-side script")
		}
		if _, err := os.Stat(filepath.Join(runner.home, ".myapp", "deploy", "old2", "compose.gen.yml")); err != nil {
			t.Error("the older artifact's deploy dir was not installed")
		}
	})

	t.Run("newer script runs an older artifact's deploy dir", func(t *testing.T) {
		runner.calls()
		_, stderr, code := runner.run(runner.home, runSide, "run", "--image", "old2", "-d")
		if code != 0 {
			t.Fatalf("run --image old2 failed:\n%s", stderr)
		}
		mustContain(t, "stderr", stderr, "schemaf v1.12.0")
		deploy := filepath.Join(runner.home, ".myapp", "deploy", "old2", "compose.gen.yml")
		mustContain(t, "calls", runner.calls(), "podman compose -f "+deploy+" up -d | SCHEMAF_IMAGE=myapp:old2")
	})

	t.Run("installed script without a readable version: refused", func(t *testing.T) {
		if _, stderr, code := runner.run(runner.home, runSide, "image", "rm", "new"); code != 0 {
			t.Fatalf("image rm new failed:\n%s", stderr)
		}
		if err := os.WriteFile(runSide, []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
			t.Fatal(err)
		}
		runner.calls()
		_, stderr, code := runner.run(runner.home, newArt, "load")
		if code == 0 {
			t.Fatal("load replaced a script whose version it can't read")
		}
		mustContain(t, "stderr", stderr, "names no schemaf version")
		mustNotContain(t, "calls", runner.calls(), "podman load")
		if readFile(t, runSide) != "#!/bin/bash\necho hi\n" {
			t.Error("the unreadable script was changed")
		}
	})
}

// TestShellVersionCmp checks _schemaf_version_cmp, which load uses to decide
// whether to replace the run-side script.
func TestShellVersionCmp(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	script := renderSchemafSh(t, t.TempDir())
	fn := shellFunc(t, script, "_schemaf_version_cmp") + shellFunc(t, script, "_schemaf_prerelease_cmp")
	for _, tc := range []struct{ a, b, want string }{
		{"v1.2.3", "v1.2.3", "0"},
		{"v1.12.0", "v1.9.0", "1"},
		{"v1.9.0", "v1.12.0", "-1"},
		{"v2.0.0", "v1.99.99", "1"},
		{"v1.2.10", "v1.2.9", "1"},
		{"v1.2.3", "v1.2.3-rc.1", "1"},
		{"v1.2.3-rc.1", "v1.2.3", "-1"},
		{"v1.2.4-0.20261120101010-abcdef123456", "v1.2.3", "1"},
		{"v1.2.4-0.20261120101010-abcdef123456", "v1.2.4-0.20261119101010-abcdef123456", "1"},
		{"v1.2.3-rc.10", "v1.2.3-rc.9", "1"},
		{"v1.2.3-rc.9", "v1.2.3-rc.10", "-1"},
		{"v1.2.3-rc", "v1.2.3-rc.1", "-1"},
		{"v1.2.3-rc.1", "v1.2.3-1", "1"},
		{"v1.2.3-beta", "v1.2.3-alpha", "1"},
		{"v1.2.3+meta", "v1.2.3", "0"},
		{"v1.2.3", "1.2.3", "error"},
		{"(devel)", "v1.2.3", "error"},
		{"", "v1.2.3", "error"},
	} {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+fn+"_schemaf_version_cmp \"$1\" \"$2\"", "x", tc.a, tc.b)
		out, err := cmd.Output()
		got := strings.TrimSpace(string(out))
		if err != nil {
			got = "error"
		}
		if got != tc.want {
			t.Errorf("cmp(%q, %q) = %s, want %s", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckoutRunBuildsThenStarts(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	short := git(t, root, "rev-parse", "--short", "HEAD")
	_, stderr, code := u.run(root, "schemaf.sh", "run", "-d")
	if code != 0 {
		t.Fatalf("run failed:\n%s", stderr)
	}
	calls := u.calls()
	build := strings.Index(calls, "podman build")
	up := strings.Index(calls, "podman compose -f compose.gen.yml up -d | SCHEMAF_IMAGE=myapp:"+short)
	if build < 0 || up < 0 || build > up {
		t.Errorf("run must build, then start the built image:\n%s", calls)
	}
}

func TestCheckoutRunBuildSeesOnlyRuntime(t *testing.T) {
	root := newDeployProject(t)
	u := newShUser(t)
	withEnvFile(t, u, "etc/env", "SCHEMAF_RUNTIME=docker\nDB_PASS=x\nPUBLIC_FOO=y\n")
	_, stderr, code := u.run(root, "schemaf.sh", "run", "-d")
	if code != 0 {
		t.Fatalf("run failed:\n%s", stderr)
	}
	calls := u.calls()
	mustContain(t, "calls", calls, "docker build", "docker compose -f compose.gen.yml up -d")
	mustNotContain(t, "calls", calls, "podman build", "DB_PASS", "PUBLIC_FOO")
	mustNotContain(t, "build environment", readFile(t, filepath.Join(u.state, "build_env")), "DB_PASS", "PUBLIC_FOO")
}

// withEnvFile writes ~/.myapp/<rel> for user u and makes the fake docker
// usable, until the test ends.
func withEnvFile(t *testing.T, u *shUser, rel, content string) {
	t.Helper()
	p := filepath.Join(u.home, ".myapp", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	usable := filepath.Join(u.state, "docker_usable")
	if err := os.WriteFile(usable, nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(u.home, ".myapp"))
		_ = os.Remove(usable)
	})
}

func TestComposeNeverBuilds(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	s := readFile(t, "compose.gen.yml")
	backend := s[strings.Index(s, "  backend:"):]
	backend = backend[:strings.Index(backend, "\n\n")]
	mustNotContain(t, "backend service", backend, "build:", "Dockerfile")
	mustContain(t, "backend service", backend,
		`image: "${SCHEMAF_IMAGE:?set by schemaf.sh — use ./schemaf.sh run}"`, "pull_policy: never")

	// With a usable compose, check the real behaviour: config fails without
	// SCHEMAF_IMAGE and passes with it.
	cmd, err := cli.Container("compose", "-f", "compose.gen.yml", "config", "-q")
	if err != nil {
		t.Skipf("no container runtime: %v", err)
	}
	cmd.Env = withoutEnv(cmd.Env, "SCHEMAF_IMAGE")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "set by schemaf.sh") {
		if err != nil && !strings.Contains(string(out), "SCHEMAF_IMAGE") {
			t.Skipf("compose not usable here: %s", out)
		}
		t.Fatalf("compose config without SCHEMAF_IMAGE: err=%v\n%s", err, out)
	}
	cmd, _ = cli.Container("compose", "-f", "compose.gen.yml", "config", "-q")
	cmd.Env = append(withoutEnv(cmd.Env, "SCHEMAF_IMAGE"), "SCHEMAF_IMAGE=myapp:test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compose config with SCHEMAF_IMAGE: %v\n%s", err, out)
	}
}

func withoutEnv(env []string, key string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}
