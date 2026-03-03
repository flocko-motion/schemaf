package cli

import (
	"os"
	"path/filepath"
)

// ProjectPath resolves a path relative to the atlas home directory.
// Uses $ATLAS_HOME if set, otherwise defaults to $HOME/.atlas.
func ProjectPath(relativePath string) string {
	home := os.Getenv("ATLAS_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".atlas")
	}
	return filepath.Join(home, relativePath)
}

// EnsureProjectDir ensures a directory exists under the atlas home directory.
// Returns the absolute path to the directory.
func EnsureProjectDir(relativePath string) (string, error) {
	dir := ProjectPath(relativePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}
