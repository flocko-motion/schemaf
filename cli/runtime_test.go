package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stub describes a fake runtime CLI (podman or docker) placed on PATH.
type stub struct {
	infoFails  bool   // `<rt> info` exits non-zero
	compose    string // output of `<rt> compose version`
	composeErr bool   // `<rt> compose version` exits non-zero
	volume     bool   // `<rt> volume inspect <name>` succeeds
	volumeName string // if set, volume inspect succeeds only for this name (needs volume)
	machines   string // output of `podman machine list --format {{.Name}}`
	socketPath string // output of `podman info --format {{.Host.RemoteSocket.Path}}` (default: a rootless path; "-" prints nothing)
	socketDown bool   // `podman --remote --url <socket> version` fails: nothing answers
}

const defaultSocketPath = "/run/user/1000/podman/podman.sock"

// healthy is a runtime that is installed and usable, with docker-compose and no volume.
var healthy = stub{compose: "Docker Compose version v2.29.0"}

func rc(fail bool) int {
	if fail {
		return 1
	}
	return 0
}

func writeStub(t *testing.T, dir string, name Runtime, s stub) {
	t.Helper()
	socketPath := s.socketPath
	switch socketPath {
	case "":
		socketPath = defaultSocketPath
	case "-":
		socketPath = ""
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
  "info --format") printf '%%s\n' %q; exit 0 ;;
  "--remote --url") [ %d = 0 ] && exit 0; echo "Client: 1.0"; echo "Cannot connect to Podman at $3" >&2; exit 1 ;;
  "info "*) echo "info output"; exit %d ;;
  "compose version") echo %q; exit %d ;;
  "volume inspect") [ -z %q ] || [ "$3" = %q ] || exit 1; exit %d ;;
  "machine list") printf '%%s' %q; exit 0 ;;
esac
exit 0
`, socketPath, rc(s.socketDown), rc(s.infoFails), s.compose, rc(s.composeErr), s.volumeName, s.volumeName, rc(!s.volume), s.machines)
	if err := os.WriteFile(filepath.Join(dir, string(name)), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

// fakeRuntimes puts only the given stubs on PATH and selects runtime sel
// ("" leaves SCHEMAF_RUNTIME unset).
func fakeRuntimes(t *testing.T, sel string, stubs map[Runtime]stub) {
	t.Helper()
	dir := t.TempDir()
	for name, s := range stubs {
		writeStub(t, dir, name, s)
	}
	t.Setenv("PATH", dir)
	t.Setenv("DOCKER_HOST", "") // restored after the test …
	os.Unsetenv("DOCKER_HOST")  // … but unset during it
	t.Setenv(RuntimeEnv, sel)
	if sel == "" {
		os.Unsetenv(RuntimeEnv)
	}
	old := goos
	goos = "linux"
	t.Cleanup(func() { goos = old })
}

func TestSelectedRuntime(t *testing.T) {
	for _, tc := range []struct {
		value   string
		unset   bool
		want    Runtime
		wantErr bool
	}{
		{unset: true, want: Podman},
		{value: "", want: Podman},
		{value: "podman", want: Podman},
		{value: "docker", want: Docker},
		{value: "Docker", wantErr: true},
		{value: "containerd", wantErr: true},
	} {
		t.Run(fmt.Sprintf("%q unset=%v", tc.value, tc.unset), func(t *testing.T) {
			t.Setenv(RuntimeEnv, tc.value)
			if tc.unset {
				os.Unsetenv(RuntimeEnv)
			}
			got, err := SelectedRuntime()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				for _, s := range []string{tc.value, "podman", "docker"} {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q should mention %q", err, s)
					}
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestContainerUsesSelectedRuntime(t *testing.T) {
	fakeRuntimes(t, "docker", map[Runtime]stub{Docker: healthy, Podman: healthy})
	cmd, err := Container("compose", "ps")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cmd.Path) != "docker" || strings.Join(cmd.Args[1:], " ") != "compose ps" {
		t.Fatalf("got %s %v", cmd.Path, cmd.Args)
	}

	fakeRuntimes(t, "bogus", map[Runtime]stub{Docker: healthy, Podman: healthy})
	if _, err := Container("ps"); err == nil {
		t.Fatal("invalid SCHEMAF_RUNTIME must fail")
	}

	fakeRuntimes(t, "", map[Runtime]stub{Docker: healthy})
	if _, err := Container("ps"); err == nil || !strings.Contains(err.Error(), "podman is not installed") {
		t.Fatalf("missing podman must fail loudly, got %v", err)
	}
}

func TestMissingToolMessages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sel   string
		goos  string
		stubs map[Runtime]stub
		want  []string
	}{
		{
			name:  "podman not installed",
			stubs: map[Runtime]stub{},
			want:  []string{"podman is not installed", "SCHEMAF_RUNTIME=docker"},
		},
		{
			name:  "podman compose uses podman-compose",
			stubs: map[Runtime]stub{Podman: {compose: "podman-compose version 1.0.6"}},
			want:  []string{"podman-compose", "install docker-compose"},
		},
		{
			name:  "podman compose has no provider",
			stubs: map[Runtime]stub{Podman: {compose: "Error: looking up compose provider failed", composeErr: true}},
			want:  []string{"no compose provider", "install docker-compose"},
		},
		{
			name:  "macOS: podman machine not running",
			goos:  "darwin",
			stubs: map[Runtime]stub{Podman: {infoFails: true, compose: healthy.compose, machines: "podman-machine-default*\n"}},
			want:  []string{"not running", "podman machine start"},
		},
		{
			name:  "macOS: no podman machine",
			goos:  "darwin",
			stubs: map[Runtime]stub{Podman: {infoFails: true, compose: healthy.compose}},
			want:  []string{"no podman machine", "podman machine init", "podman machine start"},
		},
		{
			name:  "linux: podman not usable",
			stubs: map[Runtime]stub{Podman: {infoFails: true, compose: healthy.compose}},
			want:  []string{"podman is installed, but not usable", "info output"},
		},
		{
			name:  "docker not installed",
			sel:   "docker",
			stubs: map[Runtime]stub{Podman: healthy},
			want:  []string{"docker is not installed", "unset SCHEMAF_RUNTIME"},
		},
		{
			name:  "docker daemon not running",
			sel:   "docker",
			stubs: map[Runtime]stub{Docker: {infoFails: true, compose: healthy.compose}},
			want:  []string{"Docker daemon is not running", "systemctl start docker"},
		},
		{
			name:  "docker compose missing",
			sel:   "docker",
			stubs: map[Runtime]stub{Docker: {compose: "docker: 'compose' is not a docker command.", composeErr: true}},
			want:  []string{"docker compose is not available", "Compose plugin"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRuntimes(t, tc.sel, tc.stubs)
			if tc.goos != "" {
				goos = tc.goos
			}
			err := CheckRuntime("myapp", 0)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			for _, s := range tc.want {
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(s)) {
					t.Errorf("error should mention %q:\n%v", s, err)
				}
			}
		})
	}
}

func TestVolumeCheck(t *testing.T) {
	withVol := healthy
	withVol.volume = true
	unusableWithVol := withVol
	unusableWithVol.infoFails = true

	for _, sel := range []Runtime{Podman, Docker} {
		other := sel.Other()
		for _, tc := range []struct {
			name    string
			stubs   map[Runtime]stub
			refuse  bool
			mention string
		}{
			{name: "neither has it", stubs: map[Runtime]stub{sel: healthy, other: healthy}},
			{name: "only selected has it", stubs: map[Runtime]stub{sel: withVol, other: healthy}},
			{name: "only other has it", stubs: map[Runtime]stub{sel: healthy, other: withVol}, refuse: true},
			{name: "both have it", stubs: map[Runtime]stub{sel: withVol, other: withVol}, refuse: true, mention: "as well"},
			{name: "other CLI absent", stubs: map[Runtime]stub{sel: healthy}},
			{name: "other CLI not usable", stubs: map[Runtime]stub{sel: healthy, other: unusableWithVol}},
		} {
			t.Run(fmt.Sprintf("%s selected/%s", sel, tc.name), func(t *testing.T) {
				fakeRuntimes(t, string(sel), tc.stubs)
				err := CheckRuntime("myapp", 0)
				if !tc.refuse {
					if err != nil {
						t.Fatalf("want start, got: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("want refusal, got nil")
				}
				msg := err.Error()
				for _, s := range []string{
					"refusing to start",
					"myapp-postgres-data",
					fmt.Sprintf("%s exec myapp-postgres pg_dump -U schemaf -d myapp", other),
					fmt.Sprintf("%s exec -i myapp-postgres psql", sel),
					fmt.Sprintf("%s volume rm myapp-postgres-data", other),
					"SCHEMAF_RUNTIME",
					tc.mention,
				} {
					if !strings.Contains(msg, s) {
						t.Errorf("refusal should mention %q:\n%s", s, msg)
					}
				}
			})
		}
	}
}

func TestPostgresVolume(t *testing.T) {
	if got := PostgresVolume("myapp", 0); got != "myapp-postgres-data" {
		t.Errorf("default: %q", got)
	}
	if got := PostgresVolume("myapp", 349); got != "myapp-349-postgres-data" {
		t.Errorf("instance 349: %q", got)
	}
}

// An instance checks its own volume: the default volume in the other runtime
// doesn't stop it, its own volume there does.
func TestVolumeCheckPerInstance(t *testing.T) {
	defaultVol := healthy
	defaultVol.volume, defaultVol.volumeName = true, "myapp-postgres-data"
	instanceVol := healthy
	instanceVol.volume, instanceVol.volumeName = true, "myapp-349-postgres-data"

	fakeRuntimes(t, "podman", map[Runtime]stub{Podman: healthy, Docker: defaultVol})
	if err := CheckRuntime("myapp", 349); err != nil {
		t.Fatalf("instance 349 with only the default volume in docker: want start, got %v", err)
	}
	if err := CheckRuntime("myapp", 0); err == nil {
		t.Fatal("default instance with its volume in docker: want refusal")
	}

	fakeRuntimes(t, "podman", map[Runtime]stub{Podman: healthy, Docker: instanceVol})
	if err := CheckRuntime("myapp", 0); err != nil {
		t.Fatalf("default instance with only instance 349's volume in docker: want start, got %v", err)
	}
	err := CheckRuntime("myapp", 349)
	if err == nil {
		t.Fatal("instance 349 with its volume in docker: want refusal")
	}
	for _, s := range []string{"refusing to start", "myapp-349-postgres-data", "docker volume rm myapp-349-postgres-data", "instances rm 349", "SCHEMAF_RUNTIME"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("refusal should mention %q:\n%s", s, err)
		}
	}
}

// Under podman on Linux, docker-compose reaches podman only through its API
// socket: CheckRuntime refuses when it doesn't answer, says where it looked
// and how to start it, and never starts it itself.
func TestPodmanSocketCheck(t *testing.T) {
	down := healthy
	down.socketDown = true
	noPath := healthy
	noPath.socketPath = "-"

	for _, tc := range []struct {
		name       string
		sel        string
		goos       string
		dockerHost string
		stubs      map[Runtime]stub
		want       []string // nil: CheckRuntime passes
	}{
		{name: "socket answers", stubs: map[Runtime]stub{Podman: healthy}},
		{
			name:  "socket down",
			stubs: map[Runtime]stub{Podman: down},
			want: []string{"podman's API socket isn't running: nothing answers at unix://" + defaultSocketPath,
				"systemctl --user enable --now podman.socket", "Cannot connect to Podman at unix://" + defaultSocketPath},
		},
		{
			name:       "DOCKER_HOST set, nothing answers there",
			dockerHost: "unix:///tmp/elsewhere.sock",
			stubs:      map[Runtime]stub{Podman: down},
			want:       []string{"nothing answers at unix:///tmp/elsewhere.sock", "The address comes from DOCKER_HOST"},
		},
		{
			name:       "DOCKER_HOST set but empty",
			dockerHost: "<empty>",
			stubs:      map[Runtime]stub{Podman: healthy},
			want:       []string{PodmanSocketEmptyDockerHost},
		},
		{
			name:  "podman reports no socket path",
			stubs: map[Runtime]stub{Podman: noPath},
			want:  []string{"podman info doesn't report its path"},
		},
		{name: "macOS: the podman machine provides it", goos: "darwin", stubs: map[Runtime]stub{Podman: down}},
		{name: "docker: no podman socket needed", sel: "docker", stubs: map[Runtime]stub{Docker: down}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeRuntimes(t, tc.sel, tc.stubs)
			if tc.goos != "" {
				goos = tc.goos
			}
			switch tc.dockerHost {
			case "": // unset (fakeRuntimes)
			case "<empty>":
				t.Setenv("DOCKER_HOST", "")
			default:
				t.Setenv("DOCKER_HOST", tc.dockerHost)
			}
			err := CheckRuntime("myapp", 0)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("want pass, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want error, got nil")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error should contain %q:\n%v", w, err)
				}
			}
			if strings.Contains(err.Error(), "Client: 1.0") {
				t.Errorf("error quotes podman's stdout (the client part), want only its stderr:\n%v", err)
			}
		})
	}
}
