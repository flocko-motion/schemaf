package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/constants"
)

// TestNewReadsProjectNameFromToml verifies that cli.New (the path every non-
// bootstrap command takes, including `test`) loads the project name from
// schemaf.toml when nothing is compiled in — instead of panicking.
func TestNewReadsProjectNameFromToml(t *testing.T) {
	constants.SetProjectName("") // framework CLI: no constants.gen.go compiled in
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schemaf.toml"),
		[]byte("title = \"X\"\nname = \"tomlapp\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("New returned nil CLI")
	}
	if got := constants.ProjectName(); got != "tomlapp" {
		t.Errorf("project name = %q, want tomlapp (should come from schemaf.toml)", got)
	}
}

// TestNewUnconfiguredReturnsActionableError verifies that running a non-bootstrap
// command in a directory with no schemaf.toml yields an actionable error telling
// the user to run `schemaf init`, rather than a panic.
func TestNewUnconfiguredReturnsActionableError(t *testing.T) {
	constants.SetProjectName("")
	t.Chdir(t.TempDir())

	_, err := New()
	if err == nil {
		t.Fatal("expected an error for an unconfigured project")
	}
	if !strings.Contains(err.Error(), "schemaf init") {
		t.Errorf("want an actionable 'schemaf init' error, got: %v", err)
	}
}
