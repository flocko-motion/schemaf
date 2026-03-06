package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindProjectRoot walks up from the current directory looking for schemaf.toml
// and returns the directory that contains it.
func FindProjectRoot() (string, error) {
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
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("schemaf.toml not found (walked up from %s)", dir)
}

// ReadProjectName walks up from the current directory looking for schemaf.toml
// and returns the value of the `name` field.
func ReadProjectName() (string, error) {
	root, err := FindProjectRoot()
	if err != nil {
		return "", err
	}
	return readNameFromTOML(filepath.Join(root, "schemaf.toml"))
}

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

// ProjectHome returns ~/.<name> — the per-project home directory.
func ProjectHome(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "."+name)
}

// EtcDir returns the config/secrets directory for the project.
// prod: ~/.<name>/etc/   dev: ~/.<name>/dev/etc/
func EtcDir(projectHome string, dev bool) string {
	if dev {
		return filepath.Join(projectHome, "dev", "etc")
	}
	return filepath.Join(projectHome, "etc")
}

// VarDir returns the runtime data directory for the project.
// prod: ~/.<name>/var/   dev: ~/.<name>/dev/var/
func VarDir(projectHome string, dev bool) string {
	if dev {
		return filepath.Join(projectHome, "dev", "var")
	}
	return filepath.Join(projectHome, "var")
}

// LoadEnv reads KEY=VALUE lines from <etcDir>/env and sets them in the process
// environment (only if the variable is not already set).
func LoadEnv(etcDir string) error {
	path := filepath.Join(etcDir, "env")
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

// EnsureProjectDir ensures a directory exists under the schemaf home directory.
// Returns the absolute path to the directory.
func EnsureProjectDir(relativePath string) (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".schemaf", relativePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}
