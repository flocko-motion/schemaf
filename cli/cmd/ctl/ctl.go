// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package ctl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cli "github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/files"
	"github.com/spf13/cobra"
)

// SubcommandProvider returns the ctl subcommand tree.
func SubcommandProvider(ctx *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctl",
		Short: "Control project services",
		Long: `Resolve, merge and run multi-service Docker Compose stacks.

Each service has its own compose file declaring dependencies via x-schemaf.
The ctl tool resolves the dependency graph and delegates to Docker Compose.`,
	}

	cmd.AddCommand(newStartCmd(ctx))
	cmd.AddCommand(newDevCmd(ctx))
	cmd.AddCommand(newStopCmd(ctx))
	cmd.AddCommand(newStatusCmd(ctx))

	return []*cobra.Command{cmd}
}

// ComposeSubcommandProvider is a compatibility wrapper for older imports.
func ComposeSubcommandProvider(ctx *cli.Context) []*cobra.Command {
	return SubcommandProvider(ctx)
}

// buildDockerComposeArgs builds the -f flags for docker compose from resolved files
func buildDockerComposeArgs(files []*ComposeFile) []string {
	var args []string
	for _, cf := range files {
		args = append(args, "-f", cf.Path)
	}
	return args
}

// resolveAndPrint resolves a compose file path and prints the resolution.
func resolveAndPrint(path string) ([]*ComposeFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("compose file not found: %s", abs)
	}

	files, err := Resolve([]string{abs})
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	cli.Info("Resolved %d compose file(s):", len(files))
	for _, cf := range files {
		fmt.Printf("  %s\n", cf.Path)
	}
	fmt.Println()

	return files, nil
}

// dockerEnv returns the environment to use for docker commands.
// On WSL2 with Docker Desktop, the default credsStore "desktop.exe" fails
// when called from inside WSL2. We detect this and transparently redirect
// DOCKER_CONFIG to ~/.schemaf/docker with a clean config (no credsStore).
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

// runDockerCompose runs docker compose with the given arguments, inheriting stdio.
func runDockerCompose(args []string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = dockerEnv()
	return cmd.Run()
}

// runDockerComposeCapture runs docker compose and returns stdout as a string.
func runDockerComposeCapture(args []string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Stderr = os.Stderr
	cmd.Env = dockerEnv()
	out, err := cmd.Output()
	return string(out), err
}

// loadProjectEnv loads secrets from ~/.<name>/etc/env into the process environment.
// Variables already set in the environment are not overwritten.
func loadProjectEnv(homeDir string, dev bool) {
	etcDir := files.ConfigDir()
	if err := files.LoadEnv(etcDir); err != nil {
		cli.Warning("loading env from %s: %v", etcDir, err)
	}
}

// runNativeStop executes the native-stop command for a service (fire and forget).
func runNativeStop(svcName string, schemaf *SchemafExtension) {
	if schemaf == nil || schemaf.NativeStop == "" {
		return
	}
	cli.Info("Stopping native %s: %s", svcName, schemaf.NativeStop)
	cmd := exec.Command("bash", "-c", schemaf.NativeStop)
	// Ignore errors — service may not be running
	_ = cmd.Run()
}

// stopContainer stops a docker container by name (for --native handoff).
func stopContainer(containerName string) {
	cli.Info("Stopping container: %s", containerName)
	cmd := exec.Command("docker", "stop", containerName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// parseShortNames parses a comma-separated list of short names and resolves
// them to compose service names using the short-name map.
func parseShortNames(input string, shortMap map[string]string) ([]string, error) {
	var result []string
	for _, s := range strings.Split(input, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		svcName, ok := shortMap[s]
		if !ok {
			// Try direct service name match
			svcName = s
			_ = ok
		}
		result = append(result, svcName)
	}
	return result, nil
}

// runShell executes a shell script string in a given working directory,
// inheriting stdio so the user sees output in real time.
func runShell(script, dir string) error {
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// injectProjectEnv reads x-schemaf metadata from the entry (last) compose file
// and injects environment variables before docker compose runs:
//   - PROJECT_NAME  from x-schemaf.project
//   - DB_PASS       from x-schemaf.dev-db-pass (only when DB_PASS is not already set)
func injectProjectEnv(files []*ComposeFile) {
	if len(files) == 0 {
		return
	}
	entry := files[len(files)-1]
	if entry.Schemaf == nil || entry.Schemaf.Project == "" {
		cli.Warning("x-schemaf.project not set in entry compose file; PROJECT_NAME not injected")
		return
	}
	os.Setenv("PROJECT_NAME", entry.Schemaf.Project)
	cli.Info("PROJECT_NAME=%s", entry.Schemaf.Project)

	if entry.Schemaf.DevDBPass != "" && os.Getenv("DB_PASS") == "" {
		os.Setenv("DB_PASS", entry.Schemaf.DevDBPass)
		cli.Info("DB_PASS=<from dev-db-pass>")
	}
}

// difference returns elements in all that are not in include.
func difference(all, include []string) []string {
	set := map[string]bool{}
	for _, s := range include {
		set[s] = true
	}
	var diff []string
	for _, s := range all {
		if !set[s] {
			diff = append(diff, s)
		}
	}
	return diff
}
