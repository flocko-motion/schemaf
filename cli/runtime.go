// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flocko-motion/schemaf/files"
)

// RuntimeEnv is the environment variable that selects the container runtime.
// The runtime is a property of the machine, not the project — see
// INSTALL.md › Container runtime.
const RuntimeEnv = "SCHEMAF_RUNTIME"

// Runtime is a container runtime CLI: podman (the default) or docker.
type Runtime string

const (
	Podman Runtime = "podman"
	Docker Runtime = "docker"
)

// goos is runtime.GOOS, swappable in tests.
var goos = runtime.GOOS

// SelectedRuntime returns the runtime chosen by SCHEMAF_RUNTIME: podman when
// unset or "podman", docker when "docker". Any other value is an error.
func SelectedRuntime() (Runtime, error) {
	switch v := os.Getenv(RuntimeEnv); v {
	case "", string(Podman):
		return Podman, nil
	case string(Docker):
		return Docker, nil
	default:
		return "", fmt.Errorf("%s=%q is not a valid container runtime — valid values: podman (the default), docker", RuntimeEnv, v)
	}
}

// Other returns the runtime that is not r.
func (r Runtime) Other() Runtime {
	if r == Docker {
		return Podman
	}
	return Docker
}

// Container returns a command that runs the selected runtime's CLI with args,
// e.g. Container("compose", "-f", "compose.gen.yml", "up"). Every container
// call in schemaf goes through here. The caller wires stdio and runs it.
func Container(args ...string) (*exec.Cmd, error) {
	rt, err := SelectedRuntime()
	if err != nil {
		return nil, err
	}
	if !rt.installed() {
		return nil, rt.notInstalledError()
	}
	return rt.command(args...), nil
}

// command builds the exec.Cmd for runtime r. It is the single place where a
// container CLI process is created.
func (r Runtime) command(args ...string) *exec.Cmd {
	cmd := exec.Command(string(r), args...)
	cmd.Env = r.env()
	return cmd
}

func (r Runtime) installed() bool {
	_, err := exec.LookPath(string(r))
	return err == nil
}

// usable reports whether the runtime's engine answers (podman machine up,
// docker daemon running), returning its output for error messages.
func (r Runtime) usable() (bool, string) {
	out, err := r.command("info").CombinedOutput()
	return err == nil, strings.TrimSpace(string(out))
}

// env returns the process environment for runtime r.
func (r Runtime) env() []string {
	if r == Docker {
		return dockerEnv()
	}
	env := os.Environ()
	// podman compose announces its external provider on every call; schemaf
	// already checked which provider it is.
	if _, ok := os.LookupEnv("PODMAN_COMPOSE_WARNING_LOGS"); !ok {
		env = append(env, "PODMAN_COMPOSE_WARNING_LOGS=false")
	}
	return env
}

// CheckRuntime verifies that the selected runtime and its compose are usable,
// and that the Postgres volume of the project's dev instance (0 = default)
// exists only in the selected runtime. Called at dev / run / test startup,
// before any container is touched.
func CheckRuntime(project string, instance int) error {
	rt, err := SelectedRuntime()
	if err != nil {
		return err
	}
	if err := rt.checkEngine(); err != nil {
		return err
	}
	if err := rt.checkCompose(); err != nil {
		return err
	}
	return checkVolume(rt, project, instance)
}

// CheckEngine verifies that the selected runtime is installed and its engine
// answers (podman machine up, Docker daemon running), with the same messages
// as CheckRuntime. For callers that run single containers, not compose.
func CheckEngine() error {
	rt, err := SelectedRuntime()
	if err != nil {
		return err
	}
	return rt.checkEngine()
}

func (r Runtime) notInstalledError() error {
	if r == Podman {
		return fmt.Errorf(`podman is not installed.
schemaf runs containers with rootless podman by default. Either:
  - install podman and docker-compose (macOS: brew install podman docker-compose;
    other systems: https://podman.io/docs/installation), or
  - use Docker instead: set %s=docker (e.g. in ~/.<name>/etc/env and ~/.<name>/dev/etc/env)`, RuntimeEnv)
	}
	return fmt.Errorf(`%s=docker, but docker is not installed.
Either install Docker (https://docs.docker.com/get-docker/), or unset %s to use podman (the default)`, RuntimeEnv, RuntimeEnv)
}

// checkEngine fails when the runtime is missing or its engine doesn't answer.
func (r Runtime) checkEngine() error {
	if !r.installed() {
		return r.notInstalledError()
	}
	ok, out := r.usable()
	if ok {
		return nil
	}
	if r == Docker {
		return fmt.Errorf(`docker is installed, but the Docker daemon is not running.
Start it (Docker Desktop, or: sudo systemctl start docker), or unset %s to use podman (the default).
docker info said:
%s`, RuntimeEnv, indent(out))
	}
	if goos == "darwin" {
		names, _ := r.command("machine", "list", "--format", "{{.Name}}").Output()
		if strings.TrimSpace(string(names)) == "" {
			return fmt.Errorf(`podman is installed, but no podman machine exists.
On macOS podman runs containers in a Linux VM. Create and start it once:
  podman machine init
  podman machine start`)
		}
		return fmt.Errorf(`podman is installed, but the podman machine is not running.
Start it:
  podman machine start
podman info said:
%s`, indent(out))
	}
	return fmt.Errorf(`podman is installed, but not usable.
podman info said:
%s`, indent(out))
}

// checkCompose fails unless `<runtime> compose` is docker-compose (Compose v2)
// and, under podman, docker-compose can reach podman (checkPodmanSocket).
func (r Runtime) checkCompose() error {
	out, err := r.command("compose", "version").CombinedOutput()
	text := string(out)
	if err == nil && strings.Contains(text, "Docker Compose version") {
		return r.checkPodmanSocket()
	}
	if r == Docker {
		return fmt.Errorf(`docker compose is not available.
Install the Docker Compose plugin: https://docs.docker.com/compose/install/
docker compose version said:
%s`, indent(strings.TrimSpace(text)))
	}
	if strings.Contains(text, "podman-compose") {
		return fmt.Errorf(`podman compose uses podman-compose, but schemaf needs docker-compose.
schemaf relies on Compose v2 features podman-compose lacks (up --wait, healthy depends_on,
optional env_file, file secrets). Install docker-compose
(macOS: brew install docker-compose; other systems: https://github.com/docker/compose/releases)
and make sure podman picks it: podman compose version must print "Docker Compose version ...".`)
	}
	return fmt.Errorf(`podman compose has no compose provider. Install docker-compose
(macOS: brew install docker-compose; other systems: https://github.com/docker/compose/releases).
podman compose version said:
%s`, indent(strings.TrimSpace(text)))
}

// checkPodmanSocket fails when podman's API socket doesn't answer. podman
// compose hands off to docker-compose, which talks to podman through that
// socket: DOCKER_HOST if set, else podman's own API socket. On Linux that is
// the user's podman.socket, which nobody starts implicitly; without it
// docker-compose fails with a misleading "is the docker daemon running?".
// On macOS the podman machine provides the socket, and checkEngine has
// already checked the machine answers. Pinged with podman's remote client
// (`podman --remote --url <socket> version`), so no other tool is needed.
// Never starts anything itself.
func (r Runtime) checkPodmanSocket() error {
	if r != Podman || goos == "darwin" {
		return nil
	}
	sock, fromEnv := os.LookupEnv("DOCKER_HOST")
	if !fromEnv || sock == "" {
		fromEnv = false
		out, err := r.command("info", "--format", "{{.Host.RemoteSocket.Path}}").Output()
		sock = strings.TrimSpace(string(out))
		if err != nil || sock == "" {
			return fmt.Errorf("podman compose hands off to docker-compose, which needs podman's API socket, but podman info doesn't report its path (podman info --format '{{.Host.RemoteSocket.Path}}')")
		}
		if !strings.Contains(sock, "://") {
			sock = "unix://" + sock
		}
	}
	// stderr only: on failure, version still prints the client part to stdout.
	ping := r.command("--remote", "--url", sock, "version")
	var stderr strings.Builder
	ping.Stderr = &stderr
	if err := ping.Run(); err == nil {
		return nil
	}
	fix := PodmanSocketFixDefault
	if fromEnv {
		fix = PodmanSocketFixDockerHost
	}
	return errors.New(PodmanSocketText(sock, fix, indent(strings.TrimSpace(stderr.String()))))
}

// The last sentences of PodmanSocketText: how to fix it, depending on where
// the socket address came from.
const (
	PodmanSocketFixDefault    = "Start it once (it stays enabled):\n  systemctl --user enable --now podman.socket"
	PodmanSocketFixDockerHost = "The address comes from DOCKER_HOST: point DOCKER_HOST at podman's socket, or unset it to use podman's own (start that once: systemctl --user enable --now podman.socket)."
)

// PodmanSocketText is the refusal when podman's API socket doesn't answer:
// sock is the address tried, fix one of the PodmanSocketFix* texts, said
// podman's indented output. schemaf.sh prints the same text: codegen renders
// it with shell variables in place of the three parts.
func PodmanSocketText(sock, fix, said string) string {
	return fmt.Sprintf(`podman's API socket isn't running: nothing answers at %s.
podman compose hands off to docker-compose, which talks to podman through that socket.
%s
podman said:
%s`, sock, fix, said)
}

// PostgresVolume is the fixed name of the Postgres data volume of a project's
// dev instance (compose.gen.yml.tmpl): "<project>-postgres-data" for the
// default instance (0, also production), "<project>-<n>-postgres-data" for
// instance n (INSTALL.md › Parallel dev instances).
func PostgresVolume(project string, instance int) string {
	if instance == 0 {
		return project + "-postgres-data"
	}
	return fmt.Sprintf("%s-%d-postgres-data", project, instance)
}

func (r Runtime) hasVolume(name string) bool {
	return r.command("volume", "inspect", name).Run() == nil
}

// checkVolume refuses to start when the other runtime has the Postgres volume
// of the project's instance. If the other runtime isn't installed or usable,
// it is skipped. schemaf never migrates or deletes anything itself.
func checkVolume(rt Runtime, project string, instance int) error {
	other := rt.Other()
	if !other.installed() {
		return nil
	}
	if ok, _ := other.usable(); !ok {
		return nil
	}
	vol := PostgresVolume(project, instance)
	if !other.hasVolume(vol) {
		return nil
	}
	if instance != 0 {
		return errors.New(instanceVolumeConflictMessage(rt, project, instance))
	}
	return errors.New(VolumeConflictMessage(rt, project, rt.hasVolume(vol)))
}

// instanceVolumeConflictMessage is the refusal for a parallel dev instance.
// An instance's data is a working copy, so the way out is to remove it or to
// switch runtimes; the migration recipe of VolumeConflictMessage is for the
// default instance's (and production's) data.
func instanceVolumeConflictMessage(rt Runtime, project string, instance int) string {
	other := rt.Other()
	vol := PostgresVolume(project, instance)
	return fmt.Sprintf(`refusing to start: the Postgres volume %[3]s of instance %[4]d exists in %[2]s, but the selected container runtime is %[1]s.
An instance's data volume may exist only in the selected runtime. Choose one:

  1. Delete it from %[2]s (switch runtimes first: %[5]s, then ./schemaf.sh instances rm %[4]d),
     or directly:
       %[2]s volume rm %[3]s

  2. Or keep using %[2]s for this project: %[5]s
     (in your shell profile, or in ~/.%[6]s/dev/etc/env).`, rt, other, vol, instance, VolumeConflictSwitchHint(other), project)
}

// VolumeConflictMessage is the refusal printed when the runtime that is not
// selected (rt.Other()) holds the project's Postgres volume; selectedHasIt
// says whether rt has one too. schemaf.sh prints the same text: codegen
// renders VolumeConflictText, VolumeConflictBoth and VolumeConflictSwitchHint
// into it with shell variables in place of the runtimes.
func VolumeConflictMessage(rt Runtime, project string, selectedHasIt bool) string {
	other := rt.Other()
	both := ""
	if selectedHasIt {
		both = VolumeConflictBoth(string(rt), project)
	}
	return VolumeConflictText(string(rt), string(other), project, both, VolumeConflictSwitchHint(other))
}

// VolumeConflictBoth is the extra sentence of VolumeConflictMessage when the
// selected runtime rt has the volume as well. It starts with a newline.
func VolumeConflictBoth(rt, project string) string {
	return fmt.Sprintf("\n%s has its own %s as well, so it is unclear which holds the real data.", rt, PostgresVolume(project, 0))
}

// VolumeConflictSwitchHint says how to select other, the runtime that holds the data.
func VolumeConflictSwitchHint(other Runtime) string {
	if other == Podman {
		return fmt.Sprintf("unset %s (podman is the default)", RuntimeEnv)
	}
	return fmt.Sprintf("set %s=%s", RuntimeEnv, other)
}

// VolumeConflictText is VolumeConflictMessage with every runtime-dependent
// part passed in as a string: the selected runtime rt, the other runtime, the
// VolumeConflictBoth sentence (or "") and the VolumeConflictSwitchHint.
func VolumeConflictText(rt, other, project, both, switchHint string) string {
	return fmt.Sprintf(`refusing to start: the Postgres volume %[3]s exists in %[2]s, but the selected container runtime is %[1]s.%[5]s
The project's data volume may exist only in the selected runtime. Choose one:

  1. Migrate the data to %[1]s (a move, not a copy). From the project root (on a run host: ~/.%[4]s/deploy/<running tag>/):
       set -a; . ~/.%[4]s/etc/env; set +a          # dev: ~/.%[4]s/dev/etc/env
       export SCHEMAF_IMAGE=%[4]s:unused             # required by compose.gen.yml; only postgres starts
       %[2]s compose -f compose.gen.yml down
       %[2]s compose -f compose.gen.yml up -d --wait postgres
       %[2]s exec %[4]s-postgres pg_dump -U schemaf -d %[4]s > %[4]s.sql
       %[2]s compose -f compose.gen.yml down
       %[1]s compose -f compose.gen.yml up -d --wait postgres
       %[1]s exec -i %[4]s-postgres psql -U schemaf -d %[4]s -v ON_ERROR_STOP=1 < %[4]s.sql
       # verify the data (tables, row counts), then:
       %[1]s compose -f compose.gen.yml down
       %[2]s volume rm %[3]s
     Step by step: INSTALL.md › Migrating from Docker to podman (docs/schemaf/INSTALL.md)

  2. Or keep the data where it is and use %[2]s: %[6]s
     (in your shell profile, or in ~/.%[4]s/etc/env and ~/.%[4]s/dev/etc/env).

  3. Or, if the data is throwaway, delete it:
       %[2]s volume rm %[3]s`, rt, other, PostgresVolume(project, 0), project, both, switchHint)
}

func indent(s string) string {
	if s == "" {
		return "  (no output)"
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// dockerEnv returns the environment to use for docker commands.
// On WSL2 with Docker Desktop, the default credsStore "desktop.exe" fails
// when called from inside WSL2. We detect this and transparently redirect
// DOCKER_CONFIG to ~/.schemaf/docker with a clean config (no credsStore).
// Applies only when the runtime is Docker.
func dockerEnv() []string {
	env := os.Environ()

	// Detect WSL2 via /proc/version
	procVersion, err := os.ReadFile("/proc/version")
	if err != nil || !strings.Contains(strings.ToLower(string(procVersion)), "microsoft") {
		return env
	}

	// Check if default docker config has the problematic credsStore
	defaultConfig := filepath.Join(os.Getenv("HOME"), ".docker", "config.json")
	data, err := os.ReadFile(defaultConfig)
	if err != nil || !strings.Contains(string(data), "desktop.exe") {
		return env
	}

	// Use a clean config under ~/.schemaf/docker
	schemafDockerDir, err := files.EnsureDir(files.DockerDir())
	if err != nil {
		return env
	}
	cleanConfig := filepath.Join(schemafDockerDir, "config.json")
	if _, err := os.Stat(cleanConfig); os.IsNotExist(err) {
		_ = os.WriteFile(cleanConfig, []byte("{}"), 0644)
	}

	// Replace or append DOCKER_CONFIG in the environment
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "DOCKER_CONFIG=") {
			result = append(result, e)
		}
	}
	return append(result, "DOCKER_CONFIG="+schemafDockerDir)
}
