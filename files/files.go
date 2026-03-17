// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package files

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flocko-motion/schemaf/constants"
	slog "github.com/flocko-motion/schemaf/log"
)

// isDev returns true when not running inside Docker.
func isDev() bool {
	return os.Getenv("SCHEMAF_ENV") != "docker"
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
