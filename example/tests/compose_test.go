package tests

import (
	"path/filepath"
	"runtime"
	"testing"

	"atlas.local/base/cli/cmd/compose"
)

func TestComposeResolve(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..")

	appYML := filepath.Join(root, "compose", "app.yml")
	files, err := compose.Resolve([]string{appYML})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(files) < 3 {
		t.Errorf("expected at least 3 files (postgres, nginx, app); got %d", len(files))
	}

	// The entry file (app.yml) should be last
	last := files[len(files)-1]
	if filepath.Base(last.Path) != "app.yml" {
		t.Errorf("expected last file to be app.yml, got %s", filepath.Base(last.Path))
	}

	// First file should be postgres (dependency of nginx, dependency of app)
	first := files[0]
	if filepath.Base(first.Path) != "postgres.yml" {
		t.Errorf("expected first file to be postgres.yml, got %s", filepath.Base(first.Path))
	}
}

func TestComposeDedup(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..")

	appYML := filepath.Join(root, "compose", "app.yml")
	// Resolve twice — should not duplicate
	files, err := compose.Resolve([]string{appYML, appYML})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	seen := map[string]int{}
	for _, f := range files {
		seen[f.Path]++
	}
	for path, count := range seen {
		if count > 1 {
			t.Errorf("duplicate file: %s (count=%d)", path, count)
		}
	}
}

func TestProjectName(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..")

	appYML := filepath.Join(root, "compose", "app.yml")
	files, err := compose.Resolve([]string{appYML})
	if err != nil {
		t.Fatal(err)
	}

	entry := files[len(files)-1]
	if entry.Atlas == nil || entry.Atlas.Project == "" {
		t.Error("x-atlas.project not set in app.yml")
	}
	if entry.Atlas.Project != "atlas-example" {
		t.Errorf("project: got %q, want %q", entry.Atlas.Project, "atlas-example")
	}
}
