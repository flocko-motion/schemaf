// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"embed"
	"fmt"
	"os"
	"os/exec"

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

// validateFrontend checks that required convention files exist.
func validateFrontend() error {
	required := []string{
		"frontend/package.json",
		"frontend/vite.config.ts",
		"frontend/index.html",
	}
	for _, path := range required {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("missing %s — schemaf requires Vite+React+TypeScript frontend conventions. See EXTEND.md", path)
		}
	}
	return nil
}
