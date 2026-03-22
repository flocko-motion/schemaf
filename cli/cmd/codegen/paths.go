// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// chdirToProjectRoot finds the project root (directory containing schemaf.toml)
// and changes the working directory to it. This ensures codegen output paths
// resolve correctly regardless of where the command is invoked from.
func chdirToProjectRoot() error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	return os.Chdir(root)
}

// findProjectRoot walks up from the current directory looking for schemaf.toml
// and returns the directory that contains it.
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
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("schemaf.toml not found (walked up from %s)", dir)
}

// ReadProjectName walks up from the current directory looking for schemaf.toml
// and returns the value of the `name` field.
func readProjectName() (string, error) {
	root, err := findProjectRoot()
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
