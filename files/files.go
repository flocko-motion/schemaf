// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package files

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flocko-motion/schemaf/constants"
	slog "github.com/flocko-motion/schemaf/log"
)

// isDev returns true when not running inside Docker.
func isDev() bool {
	return os.Getenv("SCHEMAF_ENV") != "docker"
}

// EnsureProjectConfigured makes sure the project name is registered. If it was
// not compiled in via constants.gen.go (the usual case when the framework CLI
// runs on the host rather than the project's own binary), it reads name/port
// from the nearest schemaf.toml — the canonical definition of a project.
//
// Returns an actionable error if no configured schemaf.toml can be found, so
// callers can tell the user to run `schemaf init` instead of crashing.
func EnsureProjectConfigured() error {
	if constants.IsProjectNameSet() {
		return nil
	}
	root, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("project not configured: no schemaf.toml found here or in any parent directory.\n" +
			"Run `schemaf init <name>` to create a new project.")
	}
	tomlPath := filepath.Join(root, "schemaf.toml")
	name, err := readNameFromTOML(tomlPath)
	if err != nil || name == "" {
		return fmt.Errorf("project not configured: %s has no `name` field.\n"+
			"Add `name = \"...\"` (and `title`) to schemaf.toml, or run `schemaf init <name>`.", tomlPath)
	}
	constants.SetProjectName(name)
	if port, perr := readPortFromTOML(tomlPath); perr == nil {
		constants.SetPort(port)
	}
	return nil
}

// findProjectRoot walks up from the current directory looking for schemaf.toml.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "schemaf.toml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("schemaf.toml not found (walked up from %s)", dir)
		}
		dir = parent
	}
}

// readNameFromTOML returns the `name` field from a schemaf.toml file.
func readNameFromTOML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"`), nil
			}
		}
	}
	return "", fmt.Errorf("no 'name' field in %s", path)
}

// readPortFromTOML returns the `port` field from a schemaf.toml file, or the
// default port (8000) if absent.
func readPortFromTOML(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 8000, nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "port") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		}
	}
	return 8000, nil
}

// ProjectHome returns the project's home directory (~/.{project}).
// Panics if the project name has not been set via constants.SetProjectName.
func ProjectHome() string {
	name := constants.ProjectName()
	if name == "" {
		panic("files.ProjectHome called before constants.SetProjectName")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "."+name)
}

// DataDir returns the project's persistent data directory, creating it if needed.
// Dev: ~/.{project}/dev/var/   Prod: ~/.{project}/var/
func DataDir() string {
	dir := VarDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("creating data dir", "path", dir, "error", err)
		os.Exit(1)
	}
	return dir
}

// ConfigDir returns the project's config/secrets directory.
// Dev: ~/.{project}/dev/etc/   Prod: ~/.{project}/etc/
func ConfigDir() string {
	if isDev() {
		return filepath.Join(ProjectHome(), "dev", "etc")
	}
	return filepath.Join(ProjectHome(), "etc")
}

func DockerDir() string {
	return filepath.Join(ProjectHome(), "docker")
}

// VarDir returns the runtime data directory.
// Dev: ~/.{project}/dev/var/   Prod: ~/.{project}/var/
func VarDir() string {
	if isDev() {
		return filepath.Join(ProjectHome(), "dev", "var")
	}
	return filepath.Join(ProjectHome(), "var")
}

// LoadEnv reads KEY=VALUE lines from <dir>/env and sets them in the process
// environment (only if the variable is not already set).
func LoadEnv(dir string) error {
	path := filepath.Join(dir, "env")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil // no env file is fine
	}
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

// EnsureDir ensures a directory exists, creating it if needed.
// Returns the absolute path to the directory.
func EnsureDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}
	return path, nil
}
