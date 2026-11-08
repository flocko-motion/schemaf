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
    exit 0 ;;
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
    echo "$1" > "$S/last_ctx"; exit 0 ;;
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
  exec) exit 0 ;;
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

	t.Run("build never reads the env file, only SCHEMAF_RUNTIME", func(t *testing.T) {
		envDir := filepath.Join(u.home, ".myapp", "dev", "etc")
		_ = os.MkdirAll(envDir, 0755)
		if err := os.WriteFile(filepath.Join(envDir, "env"), []byte("SCHEMAF_RUNTIME=docker\nDB_PASS=secret\n"), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(filepath.Join(u.home, ".myapp"))
		_, stderr, code := u.run(root, "schemaf.sh", "build", "--force")
		// docker is "not usable" in the fake, so selecting it fails the engine check.
		if code == 0 {
			t.Fatal("build ignored SCHEMAF_RUNTIME from dev/etc/env")
		}
		mustContain(t, "stderr", stderr, "docker is installed, but not usable")
		mustNotContain(t, "calls", u.calls(), "secret")
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
		_, stderr, code := runner.run(runner.home, runSide, "db", "backup")
		if code == 0 {
			t.Fatal("app subcommand ran without a backend")
		}
		mustContain(t, "stderr", stderr, "start it first: schemaf.sh run --image <tag>")
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
