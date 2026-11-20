package files

import (
	"path/filepath"
	"testing"

	"github.com/flocko-motion/schemaf/constants"
)

// TestDirsFollowDevMode: ConfigDir and VarDir pick ~/.<name>/dev/ only in
// development mode, including for a native process outside compose.
func TestDirsFollowDevMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SCHEMAF_ENV", "") // native, outside compose: must not decide the mode
	if !constants.IsProjectNameSet() {
		constants.SetProjectName("myapp")
	}
	project := filepath.Join(home, "."+constants.ProjectName())

	for _, tc := range []struct{ dev, wantConfig, wantVar string }{
		{dev: "1", wantConfig: filepath.Join(project, "dev", "etc"), wantVar: filepath.Join(project, "dev", "var")},
		{dev: "", wantConfig: filepath.Join(project, "etc"), wantVar: filepath.Join(project, "var")},
	} {
		t.Run("SCHEMAF_DEV="+tc.dev, func(t *testing.T) {
			t.Setenv("SCHEMAF_DEV", tc.dev)
			if got := ConfigDir(); got != tc.wantConfig {
				t.Errorf("ConfigDir() = %q, want %q", got, tc.wantConfig)
			}
			if got := VarDir(); got != tc.wantVar {
				t.Errorf("VarDir() = %q, want %q", got, tc.wantVar)
			}
		})
	}
}
