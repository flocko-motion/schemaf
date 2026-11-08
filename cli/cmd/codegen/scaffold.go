// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	cli "github.com/flocko-motion/schemaf/cli"
	"github.com/spf13/cobra"
)

//go:embed scaffold/package.json.tmpl
var scaffoldPackageJSON string

//go:embed scaffold/vite.config.ts.tmpl
var scaffoldViteConfig string

//go:embed scaffold/tsconfig.json.tmpl
var scaffoldTSConfig string

//go:embed scaffold/index.html.tmpl
var scaffoldIndexHTML string

//go:embed scaffold/src/main.tsx.tmpl
var scaffoldMainTSX string

//go:embed scaffold/src/App.tsx.tmpl
var scaffoldAppTSX string

//go:embed scaffold/gitignore.tmpl
var scaffoldGitignore string

// Ensure the embed import is used.
var _ embed.FS

func newScaffoldCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "scaffold",
		Short: "Scaffold frontend if missing, validate if present",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaffoldFrontend()
		},
	}
}

func runScaffoldFrontend() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}
	port, err := readPort()
	if err != nil {
		return err
	}
	data := map[string]any{"Name": name, "Port": port, "FrontendPort": port + 2, "PostgresPort": port + 3}

	if _, err := os.Stat("frontend"); os.IsNotExist(err) {
		return scaffoldFrontend(data)
	}
	return validateFrontend()
}

// scaffoldFrontend creates a minimal React+Vite+TypeScript frontend from templates.
func scaffoldFrontend(data map[string]any) error {
	cli.Info("Scaffolding frontend...")

	if err := os.MkdirAll("frontend/src", 0755); err != nil {
		return fmt.Errorf("creating frontend/src: %w", err)
	}

	files := []struct {
		tmpl string
		path string
	}{
		{scaffoldPackageJSON, "frontend/package.json"},
		{scaffoldViteConfig, "frontend/vite.config.ts"},
		{scaffoldTSConfig, "frontend/tsconfig.json"},
		{scaffoldIndexHTML, "frontend/index.html"},
		{scaffoldMainTSX, "frontend/src/main.tsx"},
		{scaffoldAppTSX, "frontend/src/App.tsx"},
		{scaffoldGitignore, "frontend/.gitignore"},
	}

	for _, f := range files {
		if err := renderTemplate(f.tmpl, f.path, data); err != nil {
			return fmt.Errorf("scaffolding %s: %w", f.path, err)
		}
	}

	cli.Success("Scaffolded frontend/")

	// Install dependencies.
	cli.Info("Installing frontend dependencies...")
	cmd := exec.Command("npm", "install")
	cmd.Dir = "frontend"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install: %w", err)
	}
	cli.Success("Frontend dependencies installed")

	return nil
}

// validateFrontend checks that required convention files exist and that the
// runtime config setup (build target, old-browser check, config import) is in
// place. Projects scaffolded before runtime config get told what to add.
func validateFrontend() error {
	required := []string{
		"frontend/package.json",
		"frontend/vite.config.ts",
		"frontend/index.html",
		"frontend/src/main.tsx",
	}
	for _, path := range required {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("missing %s — schemaf requires Vite+React+TypeScript frontend conventions. See EXTEND.md", path)
		}
	}

	var missing []string
	for _, c := range runtimeConfigChecks {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", c.path, err)
		}
		if !c.ok.Match(raw) {
			missing = append(missing, fmt.Sprintf("  %s: %s\n%s", c.path, c.what, indent(c.snippet, "      ")))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("frontend is missing the runtime config setup (see EXTEND.md, Runtime config):\n%s",
			strings.Join(missing, "\n"))
	}
	return nil
}

// runtimeConfigChecks are the pieces of the scaffold that runtime config
// (config.gen.ts) relies on.
var runtimeConfigChecks = []struct {
	path    string
	ok      *regexp.Regexp
	what    string
	snippet string
}{
	{
		path: "frontend/vite.config.ts",
		// es2022 or newer; config.gen.ts uses top-level await.
		ok:   regexp.MustCompile(`target\s*:\s*["'](es202[2-9]|esnext)["']`),
		what: "add the build target (config.gen.ts uses top-level await)",
		snippet: `build: {
  target: "es2022",
},`,
	},
	{
		path: "frontend/index.html",
		ok:   regexp.MustCompile(`__schemaf_boot`),
		what: "add the old-browser check to <head>",
		snippet: `<script>
  // schemaf: old-browser check (see EXTEND.md, Runtime config)
  window.addEventListener("load", function () {
    if (!window.__schemaf_boot) alert("This browser is too old to run this app. Please update it.");
  });
</script>`,
	},
	{
		path:    "frontend/src/main.tsx",
		ok:      regexp.MustCompile(`import\s+["']\./api/generated/config\.gen(\.ts)?["']`),
		what:    "add as the first import (loads runtime config before the app renders)",
		snippet: `import "./api/generated/config.gen";`,
	},
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
