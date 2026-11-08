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
	machines   string // output of `podman machine list --format {{.Name}}`
}

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
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
  "info "*) echo "info output"; exit %d ;;
  "compose version") echo %q; exit %d ;;
  "volume inspect") exit %d ;;
  "machine list") printf '%%s' %q; exit 0 ;;
esac
exit 0
`, rc(s.infoFails), s.compose, rc(s.composeErr), rc(!s.volume), s.machines)
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
			err := CheckRuntime("myapp")
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
				err := CheckRuntime("myapp")
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
