package codegen

// Tests for the run side's pull flow, releases and install (INSTALL.md ›
// Pulling releases). They run the generated script with bash against a fake
// container runtime that also plays the pinned curl, jq and cosign images,
// Postgres' pg_dump/psql/pg_restore and a stateful compose. A bundle here is
// one line "SIGNED <identity> <issuer> <sha256 of the artifact>", which the
// fake cosign checks against its arguments.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakePullRuntime handles what the pull flow adds and hands the rest to the
// base fake (rt-base, fakeRuntime). State in $FAKE_STATE:
//
//	backend          "true|false myapp:<tag>": the backend container
//	db               the database's content: "+<tag>" per version that ran
//	http/<path>      served for http://fake/<path>
//	unhealthy-<tag>  that tag fails its health check
//	backup-fail, restore-fail, up-fail-<tag>  make that step fail
const fakePullRuntime = `#!/bin/bash
set -u
S="${FAKE_STATE:?}"
me=$(basename "$0")
base="$(dirname "$0")/rt-base"
case "${1:-}" in
  run)
    echo "$me $*" >> "$S/calls"
    shift; img=""; vol=""
    while [ $# -gt 0 ]; do
      case "$1" in
        --rm|-i) shift ;;
        --network|--user) shift 2 ;;
        -v) vol="$2"; shift 2 ;;
        *) img="$1"; shift; break ;;
      esac
    done
    case "$img" in
      *curlimages/curl*)
        url="${@: -1}"
        case "$url" in
          */api/health)
            [ -f "$S/backend" ] || exit 7
            read -r state image < "$S/backend"
            [ "$state" = true ] || exit 7
            [ ! -f "$S/unhealthy-${image##*:}" ] || { echo '{"status":"error"}'; exit 22; }
            echo '{"db":"ok","status":"ok"}'; exit 0 ;;
          http://fake/*)
            f="$S/http/${url#http://fake/}"; [ -f "$f" ] || { echo "curl: (22) 404" >&2; exit 22; }
            cat "$f"; exit 0 ;;
        esac
        echo "fake curl: unhandled $url" >&2; exit 6 ;;
      *jqlang/jq*) echo "Trying to pull ghcr.io/jqlang/jq:1.8.2..." >&2; exec jq "$@" ;; # a first pull talks on stderr
      *cosign*)
        dir="${vol%%:*}"; bundle="" identity="" issuer="" art=""
        while [ $# -gt 0 ]; do
          case "$1" in
            verify-blob-attestation) shift ;;
            --bundle) bundle="$dir/${2#/w/}"; shift 2 ;;
            --certificate-identity) identity="$2"; shift 2 ;;
            --certificate-oidc-issuer) issuer="$2"; shift 2 ;;
            --type) shift 2 ;;
            *) art="$dir/${1#/w/}"; shift ;;
          esac
        done
        read -r word id iss sum < "$bundle"
        have=$(shasum -a 256 "$art" | cut -d' ' -f1)
        if [ "$word" != SIGNED ] || [ "$id" != "$identity" ] || [ "$iss" != "$issuer" ]; then
          echo "Error: failed to verify certificate identity: expected SAN value \"$identity\", got \"$id\"" >&2; exit 1
        fi
        [ "$sum" = "$have" ] || { echo "Error: provided artifact digests do not match digests in statement" >&2; exit 1; }
        echo "Verified OK" >&2; exit 0 ;;
    esac
    echo "fake: unhandled run image $img" >&2; exit 1 ;;
  container)
    echo "$me $*" >> "$S/calls"
    [ -f "$S/backend" ] || { echo "Error: no such container" >&2; exit 1; }
    cat "$S/backend"; exit 0 ;;
  compose)
    echo "$me $* | SCHEMAF_IMAGE=${SCHEMAF_IMAGE:-}" >> "$S/calls"
    case " $* " in
      *" version "*) echo "Docker Compose version v2.30.0"; exit 0 ;;
      *" up "*)
        tag="${SCHEMAF_IMAGE##*:}"
        [ ! -f "$S/up-fail-$tag" ] || exit 1
        echo "true $SCHEMAF_IMAGE" > "$S/backend"
        db=$(cat "$S/db" 2>/dev/null || true)
        case "$db+" in *"+$tag+"*) ;; *) echo "$db+$tag" > "$S/db" ;; esac
        exit 0 ;;
      *" stop backend "*) [ ! -f "$S/backend" ] || { read -r _ image < "$S/backend"; echo "false $image" > "$S/backend"; }; exit 0 ;;
      *" down "*) rm -f "$S/backend"; exit 0 ;;
    esac
    exit 0 ;;
  logs) echo "$me $*" >> "$S/calls"; echo "backend: migration failed"; exit 0 ;;
  exec)
    case "$*" in
      *myapp-postgres*)
        echo "$me $*" >> "$S/calls"
        case "$*" in
          *pg_dump*) [ ! -f "$S/backup-fail" ] || exit 1; echo "DUMP $(cat "$S/db" 2>/dev/null)"; exit 0 ;;
          *psql*) [ ! -f "$S/restore-fail" ] || { echo "psql: error" >&2; exit 1; }; exit 0 ;;
          *pg_restore*) read -r _ db; echo "$db" > "$S/db"; exit 0 ;;
        esac ;;
    esac ;;
esac
exec "$base" "$@"
`

const (
	pullSigner = "https://github.com/acme/myapp/.github/workflows/release.gen.yml@refs/tags/v"
	ghIssuer   = "https://token.actions.githubusercontent.com"
)

// pullHost is a run host with the pull flow set up as INSTALL.md instructs:
// the env file and the repository's schemaf.sh as ~/.myapp/bin/schemaf.sh.
type pullHost struct {
	t       *testing.T
	u       *shUser
	script  string
	builder *shUser
	root    string
	index   []map[string]any
}

func newPullHost(t *testing.T) *pullHost {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
	if _, err := exec.LookPath("shasum"); err != nil {
		t.Skip("shasum not installed")
	}
	root := newDeployProject(t)
	h := &pullHost{t: t, u: newShUser(t), builder: newShUser(t), root: root}
	h.u.env = []string{"_SCHEMAF_TEST_HEALTH_TIMEOUT=0"}
	if err := os.Rename(filepath.Join(h.u.bin, "podman"), filepath.Join(h.u.bin, "rt-base")); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"podman": fakePullRuntime,
		"sleep":  "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(h.u.bin, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	h.setEnv("DB_PASS=secret\nSCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=stable\n")
	// The bootstrap: the repository's schemaf.sh, copied.
	h.script = filepath.Join(h.u.home, ".myapp", "bin", "schemaf.sh")
	if err := os.MkdirAll(filepath.Dir(h.script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.script, []byte(readFile(t, filepath.Join(root, "schemaf.sh"))), 0755); err != nil {
		t.Fatal(err)
	}
	return h
}

// setEnv writes ~/.myapp/etc/env (withEnvFile would remove ~/.myapp when a
// subtest ends).
func (h *pullHost) setEnv(content string) {
	h.t.Helper()
	p := filepath.Join(h.u.home, ".myapp", "etc", "env")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *pullHost) state(name string) string { return filepath.Join(h.u.state, name) }

func (h *pullHost) touch(name string) {
	h.t.Helper()
	if err := os.WriteFile(h.state(name), nil, 0644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *pullHost) rm(name string) { _ = os.Remove(h.state(name)) }

func (h *pullHost) read(name string) string {
	b, _ := os.ReadFile(h.state(name))
	return strings.TrimSpace(string(b))
}

// publish builds version v as an artifact and serves it with a bundle signed
// by identity (the release workflow on v's tag when empty) and lists it in
// releases.json as listed (v when empty). tamper changes the artifact after
// signing.
func (h *pullHost) publish(v, listed, identity string, tamper bool) {
	h.t.Helper()
	if listed == "" {
		listed = v
	}
	if identity == "" {
		identity = pullSigner + listed
	}
	art := buildArtifact(h.t, h.builder, h.root, v)
	b, err := os.ReadFile(art)
	if err != nil {
		h.t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if tamper {
		b = append(b, 'x')
	}
	dir := h.state("http")
	if err := os.MkdirAll(dir, 0755); err != nil {
		h.t.Fatal(err)
	}
	name := "myapp-" + listed + ".run"
	if err := os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
		h.t.Fatal(err)
	}
	bundle := "SIGNED " + identity + " " + ghIssuer + " " + hex.EncodeToString(sum[:]) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".sigstore.json"), []byte(bundle), 0644); err != nil {
		h.t.Fatal(err)
	}
	h.index = append(h.index, map[string]any{
		"version": listed, "prerelease": strings.Contains(listed, "-"),
		"artifact": "http://fake/" + name, "attestation": "http://fake/" + name + ".sigstore.json",
	})
	h.writeIndex()
}

func (h *pullHost) writeIndex() {
	h.t.Helper()
	b, err := json.MarshalIndent(h.index, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	h.serve("releases.json", string(b))
}

func (h *pullHost) serve(path, content string) {
	h.t.Helper()
	if err := os.MkdirAll(h.state("http"), 0755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.state("http"), path), []byte(content), 0644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *pullHost) sh(args ...string) (string, string, int) {
	h.t.Helper()
	return h.u.run(h.u.home, h.script, args...)
}

func (h *pullHost) status() map[string]any {
	h.t.Helper()
	var st map[string]any
	if err := json.Unmarshal([]byte(readFile(h.t, filepath.Join(h.u.home, ".myapp", "updates", "status.json"))), &st); err != nil {
		h.t.Fatalf("status.json: %v", err)
	}
	return st
}

func (h *pullHost) installState() string {
	st := h.status()
	in, _ := st["install"].(map[string]any)
	return in["state"].(string)
}

func (h *pullHost) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(h.u.home, ".myapp", rel))
	return err == nil
}

func TestPullConfig(t *testing.T) {
	h := newPullHost(t)
	for _, c := range []struct{ env, want string }{
		{"SCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=stable\n", "SCHEMAF_RELEASES is not set"},
		{"SCHEMAF_RELEASES=ftp://x\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=stable\n", "SCHEMAF_RELEASES=\"ftp://x\" is not an http(s) URL"},
		{"SCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_CHANNEL=stable\n", "SCHEMAF_SIGNER is not set"},
		{"SCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme\nSCHEMAF_CHANNEL=stable\n", "is neither <owner>/<repo> nor a full certificate identity"},
		{"SCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\n", "SCHEMAF_CHANNEL is not set"},
		{"SCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=beta\n", "SCHEMAF_CHANNEL=\"beta\" is not a channel — use stable or prerelease"},
		{"SCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_SIGNER_ISSUER=https://x\nSCHEMAF_CHANNEL=stable\n", "SCHEMAF_SIGNER_ISSUER goes with a full certificate identity"},
	} {
		h.setEnv(c.env)
		for _, cmd := range [][]string{{"releases"}, {"install", "1.0.0"}} {
			_, stderr, code := h.sh(cmd...)
			if code == 0 {
				t.Errorf("%s accepted env %q", cmd[0], c.env)
			}
			mustContain(t, cmd[0]+" stderr", stderr, c.want)
		}
	}

	t.Run("Linux only", func(t *testing.T) {
		h.u.env = append(h.u.env, "FAKE_UNAME=Darwin")
		defer func() { h.u.env = h.u.env[:len(h.u.env)-1] }()
		for _, cmd := range [][]string{{"releases"}, {"install", "1.0.0"}} {
			_, stderr, code := h.sh(cmd...)
			if code == 0 {
				t.Errorf("%s ran on macOS", cmd[0])
			}
			mustContain(t, "stderr", stderr, "the pull flow (releases, install) runs on Linux only")
		}
	})
}

func TestReleasesAndInstall(t *testing.T) {
	h := newPullHost(t)
	h.publish("1.4.0", "", "", false)
	h.publish("1.5.0", "", "", false)
	h.publish("1.6.0-rc.1", "", "", false)

	t.Run("releases on an empty box: only the newest stable version", func(t *testing.T) {
		stdout, stderr, code := h.sh("releases")
		if code != 0 {
			t.Fatalf("releases failed:\n%s", stderr)
		}
		if stdout != "1.5.0\n" {
			t.Errorf("releases printed %q, want only 1.5.0", stdout)
		}
		if h.exists("releases/1.4.0") {
			t.Error("an empty box downloaded an older version than the newest")
		}
	})

	// The box was set up when 1.4.0 was the newest: the index listed only it.
	full := h.index
	h.index = full[:1]
	h.writeIndex()
	if _, stderr, code := h.sh("releases"); code != 0 {
		t.Fatalf("releases failed:\n%s", stderr)
	}
	h.index = full
	h.writeIndex()

	t.Run("downloads are verified and kept", func(t *testing.T) {
		for _, v := range []string{"1.4.0", "1.5.0"} {
			if !h.exists("releases/"+v+"/myapp-"+v+".run") || !h.exists("releases/"+v+"/myapp-"+v+".run.sigstore.json") {
				t.Errorf("%s not kept", v)
			}
		}
		if h.exists("releases/1.6.0-rc.1") {
			t.Error("a prerelease was downloaded on the stable channel")
		}
		calls := h.u.calls()
		mustContain(t, "calls", calls, "curlimages/curl:8.23.0@sha256:", "jqlang/jq:1.8.2@sha256:", "cosign:v3.1.4@sha256:",
			"--certificate-identity "+pullSigner+"1.4.0 --certificate-oidc-issuer "+ghIssuer, "--type slsaprovenance1")
	})

	t.Run("the prerelease channel includes prereleases", func(t *testing.T) {
		h.setEnv("DB_PASS=secret\nSCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=prerelease\n")
		defer h.setEnv("DB_PASS=secret\nSCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=stable\n")
		stdout, stderr, code := h.sh("releases")
		if code != 0 {
			t.Fatalf("releases failed:\n%s", stderr)
		}
		if stdout != "1.6.0-rc.1\n" {
			t.Errorf("releases printed %q, want the newest on the prerelease channel", stdout)
		}
		if !h.exists("releases/1.6.0-rc.1") {
			t.Error("the prerelease was not downloaded")
		}
	})

	t.Run("install refuses what isn't downloaded, off the channel or not a version", func(t *testing.T) {
		for args, want := range map[string]string{
			"1.9.0":      "1.9.0 is not downloaded — run schemaf.sh releases first",
			"1.6.0-rc.1": "1.6.0-rc.1 is a prerelease, and this host's channel is stable",
			"latest":     "\"latest\" is not a version",
		} {
			_, stderr, code := h.sh("install", args)
			if code == 0 {
				t.Errorf("install %s succeeded", args)
			}
			mustContain(t, "install "+args, stderr, want)
		}
	})

	t.Run("first install: load, start, health check; no backup", func(t *testing.T) {
		_, stderr, code := h.sh("install", "1.4.0")
		if code != 0 {
			t.Fatalf("install failed:\n%s", stderr)
		}
		if h.read("backend") != "true myapp:1.4.0" {
			t.Errorf("backend = %q", h.read("backend"))
		}
		st := h.status()
		if st["running"] != "1.4.0" || st["previous"] != "" || h.installState() != "installed" {
			t.Errorf("status = %v", st)
		}
		calls := h.u.calls()
		mustContain(t, "calls", calls, "rt-base load -i", "compose -f "+filepath.Join(h.u.home, ".myapp/deploy/1.4.0/compose.gen.yml")+" up -d", "/api/health")
		mustNotContain(t, "calls", calls, "pg_dump")
		mustContain(t, "installs.log", readFile(t, filepath.Join(h.u.home, ".myapp/var/installs.log")), " 1.4.0 from=- installed installed 1.4.0")
	})

	t.Run("install only goes forward", func(t *testing.T) {
		_, stderr, code := h.sh("install", "1.4.0")
		if code == 0 {
			t.Fatal("install of the running version succeeded")
		}
		mustContain(t, "stderr", stderr, "1.4.0 is not newer than the running 1.4.0 — install only goes forward")
	})

	t.Run("releases keeps only versions newer than the running one", func(t *testing.T) {
		stdout, stderr, code := h.sh("releases")
		if code != 0 {
			t.Fatalf("releases failed:\n%s", stderr)
		}
		if stdout != "1.5.0\n" {
			t.Errorf("releases printed %q, want 1.5.0", stdout)
		}
	})

	t.Run("a second install while one runs is refused", func(t *testing.T) {
		lock := filepath.Join(h.u.home, ".myapp", "var", "install.lock")
		if err := os.MkdirAll(lock, 0755); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(lock)
		// Held by a live process: this test.
		if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := h.sh("install", "1.5.0")
		if code == 0 {
			t.Fatal("install ran without the lock")
		}
		mustContain(t, "stderr", stderr, "another install is running (PID "+strconv.Itoa(os.Getpid())+")")
		if _, err := os.Stat(lock); err != nil {
			t.Error("the refused install removed the other install's lock")
		}
	})

	t.Run("a stale lock is taken over and removed after the install", func(t *testing.T) {
		lock := filepath.Join(h.u.home, ".myapp", "var", "install.lock")
		if err := os.MkdirAll(lock, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lock, "pid"), []byte("999999\n"), 0644); err != nil {
			t.Fatal(err)
		}
		// 1.4.0 runs: this install takes the lock, then stops as not newer.
		_, stderr, _ := h.sh("install", "1.4.0")
		mustContain(t, "stderr", stderr, "taking over a stale install lock (PID 999999 is gone)")
		if _, err := os.Stat(lock); err == nil {
			t.Error("the lock was not removed when install ended")
		}
	})

	t.Run("upgrade: stop the backend, back up, start the new version", func(t *testing.T) {
		_, stderr, code := h.sh("install", "1.5.0")
		if code != 0 {
			t.Fatalf("install failed:\n%s", stderr)
		}
		st := h.status()
		in := st["install"].(map[string]any)
		if st["running"] != "1.5.0" || st["previous"] != "1.4.0" || in["state"] != "installed" || in["from"] != "1.4.0" {
			t.Errorf("status = %v", st)
		}
		backup, _ := in["backup"].(string)
		if !strings.Contains(backup, "/.myapp/var/backups/1.4.0-") || readFile(t, backup) != "DUMP +1.4.0\n" {
			t.Errorf("backup %q holds %q", backup, readFile(t, backup))
		}
		calls := h.u.calls()
		stop := strings.Index(calls, "stop backend | SCHEMAF_IMAGE=myapp:1.4.0")
		dump := strings.Index(calls, "pg_dump")
		up := strings.Index(calls, "up -d | SCHEMAF_IMAGE=myapp:1.5.0")
		if stop < 0 || dump < stop || up < dump {
			t.Errorf("want stop backend → pg_dump → up, calls:\n%s", calls)
		}
	})

	h.publish("1.6.0", "", "", false)
	if _, stderr, code := h.sh("releases"); code != 0 {
		t.Fatalf("releases failed:\n%s", stderr)
	}

	t.Run("a failing health check rolls back and restores the database", func(t *testing.T) {
		h.touch("unhealthy-1.6.0")
		defer h.rm("unhealthy-1.6.0")
		_, stderr, code := h.sh("install", "1.6.0")
		if code == 0 {
			t.Fatal("install of an unhealthy version succeeded")
		}
		mustContain(t, "stderr", stderr, "1.6.0 failed its health check; rolled back to 1.5.0, the database restored from the backup")
		if h.read("backend") != "true myapp:1.5.0" {
			t.Errorf("backend = %q, want 1.5.0 serving again", h.read("backend"))
		}
		if h.read("db") != "+1.4.0+1.5.0" {
			t.Errorf("database = %q, want the backup's content without 1.6.0", h.read("db"))
		}
		st := h.status()
		in := st["install"].(map[string]any)
		if st["running"] != "1.5.0" || st["previous"] != "1.4.0" || in["state"] != "rolled-back" {
			t.Errorf("status = %v", st)
		}
		if log, _ := in["log"].(string); !strings.Contains(readFile(t, log), "migration failed") {
			t.Errorf("the new version's logs are not saved: %q", log)
		}
	})

	t.Run("a failing rollback stops the stack and says so", func(t *testing.T) {
		h.touch("unhealthy-1.6.0")
		h.touch("restore-fail")
		defer h.rm("unhealthy-1.6.0")
		defer h.rm("restore-fail")
		_, stderr, code := h.sh("install", "1.6.0")
		if code == 0 {
			t.Fatal("install succeeded")
		}
		mustContain(t, "stderr", stderr, "1.6.0 failed its health check, and the rollback to 1.5.0 failed too — the stack is stopped")
		if h.read("backend") != "" {
			t.Errorf("backend = %q, want the stack stopped", h.read("backend"))
		}
		if h.installState() != "rollback-failed" {
			t.Errorf("state = %q", h.installState())
		}
	})

	// Back to a running 1.5.0 for the rest (the operator's manual start).
	if _, stderr, code := h.sh("run", "--image", "1.5.0", "-d"); code != 0 {
		t.Fatalf("run --image 1.5.0:\n%s", stderr)
	}

	t.Run("a failing backup changes nothing", func(t *testing.T) {
		h.touch("backup-fail")
		defer h.rm("backup-fail")
		_, stderr, code := h.sh("install", "1.6.0")
		if code == 0 {
			t.Fatal("install succeeded without a backup")
		}
		mustContain(t, "stderr", stderr, "the database backup failed; 1.5.0 was started again, nothing changed")
		if h.read("backend") != "true myapp:1.5.0" {
			t.Errorf("backend = %q", h.read("backend"))
		}
		if h.installState() != "failed" {
			t.Errorf("state = %q", h.installState())
		}
	})

	t.Run("retention: keep running and previous, prune the rest", func(t *testing.T) {
		_, stderr, code := h.sh("install", "1.6.0")
		if code != 0 {
			t.Fatalf("install failed:\n%s", stderr)
		}
		for _, rel := range []string{"deploy/1.4.0", "releases/1.4.0"} {
			if h.exists(rel) {
				t.Errorf("%s not pruned", rel)
			}
		}
		for _, rel := range []string{"deploy/1.5.0", "releases/1.5.0", "deploy/1.6.0", "releases/1.6.0"} {
			if !h.exists(rel) {
				t.Errorf("%s pruned", rel)
			}
		}
		mustContain(t, "calls", h.u.calls(), "rt-base image rm myapp:1.4.0")
		backups, _ := filepath.Glob(filepath.Join(h.u.home, ".myapp/var/backups/*"))
		if len(backups) != 1 || !strings.Contains(backups[0], "/1.5.0-") {
			t.Errorf("backups = %v, want only this install's", backups)
		}
	})
}

func TestReleasesRefuseForeignArtifacts(t *testing.T) {
	h := newPullHost(t)
	// A box running 1.3.0, so every later version is a candidate.
	h.publish("1.3.0", "", "", false)
	if _, stderr, code := h.sh("releases"); code != 0 {
		t.Fatalf("releases:\n%s", stderr)
	}
	if _, stderr, code := h.sh("install", "1.3.0"); code != 0 {
		t.Fatalf("install 1.3.0:\n%s", stderr)
	}
	h.publish("1.4.0", "", "", false)
	h.publish("1.5.0", "", "", true)                                                                        // tampered after signing
	h.publish("1.6.0", "", "https://github.com/evil/myapp/.github/workflows/x.yml@refs/tags/v1.6.0", false) // another signer
	h.publish("1.7.0", "1.8.0", "", false)                                                                  // listed as 1.8.0, built as 1.7.0

	stdout, stderr, code := h.sh("releases")
	if code == 0 {
		t.Fatal("releases succeeded with foreign artifacts")
	}
	if stdout != "1.4.0\n" {
		t.Errorf("releases printed %q, want only the genuine 1.4.0", stdout)
	}
	mustContain(t, "stderr", stderr,
		"1.5.0: the attestation does not verify for signer acme/myapp — deleted, not kept",
		"provided artifact digests do not match",
		"1.6.0: the attestation does not verify for signer acme/myapp",
		"1.8.0: the artifact's manifest names myapp 1.7.0, the index says myapp 1.8.0 — deleted, not kept",
		"some releases failed their checks")
	for _, v := range []string{"1.5.0", "1.6.0", "1.8.0"} {
		if h.exists("releases/" + v) {
			t.Errorf("%s was kept", v)
		}
	}

	t.Run("a full certificate identity", func(t *testing.T) {
		h.setEnv("DB_PASS=x\nSCHEMAF_RELEASES=http://fake/releases.json\nSCHEMAF_SIGNER=https://github.com/evil/myapp/.github/workflows/x.yml@refs/tags/v1.6.0\nSCHEMAF_CHANNEL=stable\n")
		_, stderr, _ := h.sh("releases")
		mustContain(t, "stderr", stderr, "1.6.0: verified")
	})

	t.Run("an invalid index is refused as a whole", func(t *testing.T) {
		h.setEnv("DB_PASS=x\nSCHEMAF_RELEASES=http://fake/bad.json\nSCHEMAF_SIGNER=acme/myapp\nSCHEMAF_CHANNEL=stable\n")
		for content, want := range map[string]string{
			`{"version": "1.0.0"}`: "is not a valid release index",
			`[{"version": "1.0.0", "artifact": "http://a", "attestation": "http://b"}, {"version": "1.0.0", "artifact": "http://a", "attestation": "http://b"}]`: "a version is listed twice",
			`[{"version": "v1.0.0", "artifact": "http://a", "attestation": "http://b"}]`:                                                                         "lists \"v1.0.0\", which is not a version",
			`[{"version": "1.0.0", "artifact": "file:///etc/passwd", "attestation": "http://b"}]`:                                                                "version 1.0.0 has no http(s) URLs",
		} {
			h.serve("bad.json", content)
			_, stderr, code := h.sh("releases")
			if code == 0 {
				t.Errorf("index %s accepted", content)
			}
			mustContain(t, "stderr", stderr, want)
		}
	})
}

func TestFirstInstallFailureStopsTheNewVersion(t *testing.T) {
	h := newPullHost(t)
	h.publish("1.4.0", "", "", false)
	if _, stderr, code := h.sh("releases"); code != 0 {
		t.Fatalf("releases:\n%s", stderr)
	}
	h.touch("unhealthy-1.4.0")
	_, stderr, code := h.sh("install", "1.4.0")
	if code == 0 {
		t.Fatal("install succeeded")
	}
	mustContain(t, "stderr", stderr, "1.4.0 failed its health check; nothing to roll back to, so it is stopped (the database volume is kept)")
	if h.read("backend") != "" || h.installState() != "failed" {
		t.Errorf("backend %q, state %q", h.read("backend"), h.installState())
	}
	mustNotContain(t, "calls", h.u.calls(), "volume rm")
}

func TestPullRefusesNonVersionRunningTag(t *testing.T) {
	h := newPullHost(t)
	h.publish("1.4.0", "", "", false)
	if err := os.WriteFile(h.state("backend"), []byte("true myapp:3f9a2c1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := h.sh("releases")
	if code == 0 {
		t.Fatal("releases compared against a commit tag")
	}
	mustContain(t, "stderr", stderr, "the running tag 3f9a2c1 is not a release version")
}

func TestJSONString(t *testing.T) {
	dir := t.TempDir()
	script := renderSchemafSh(t, dir)
	fn := shellFunc(t, script, "_schemaf_json_str")
	out, err := exec.Command("bash", "-c", fn+"\n_schemaf_json_str \"$1\"", "_", "a \"q\" \\ b\nc\td\x01").Output()
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if err := json.Unmarshal(out, &s); err != nil || s != "a \"q\" \\ b\nc\td" {
		t.Errorf("_schemaf_json_str gave %s (%q, %v)", out, s, err)
	}
}
