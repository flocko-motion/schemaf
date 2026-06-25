package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/constants"
)

// TestEnsureProjectConfiguredReadsToml verifies the host-CLI fallback: when no
// project name is compiled in, EnsureProjectConfigured reads name/port from
// schemaf.toml in the current directory.
func TestEnsureProjectConfiguredReadsToml(t *testing.T) {
	constants.SetProjectName("") // simulate framework CLI: nothing compiled in
	dir := t.TempDir()
	toml := "title = \"My App\"\nname = \"myapp\"\nport = 9000\n"
	if err := os.WriteFile(filepath.Join(dir, "schemaf.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if err := EnsureProjectConfigured(); err != nil {
		t.Fatalf("EnsureProjectConfigured: %v", err)
	}
	if got := constants.ProjectName(); got != "myapp" {
		t.Errorf("name = %q, want myapp", got)
	}
	if got := constants.Port(); got != 9000 {
		t.Errorf("port = %d, want 9000", got)
	}
}

// TestEnsureProjectConfiguredMissingTomlIsActionable verifies that a genuinely
// unconfigured directory yields an actionable error pointing at `schemaf init`,
// not a panic.
func TestEnsureProjectConfiguredMissingTomlIsActionable(t *testing.T) {
	constants.SetProjectName("")
	t.Chdir(t.TempDir())

	err := EnsureProjectConfigured()
	if err == nil {
		t.Fatal("expected an error when no schemaf.toml is present")
	}
	if !strings.Contains(err.Error(), "schemaf init") {
		t.Errorf("error should tell the user to run `schemaf init`, got: %v", err)
	}
}
