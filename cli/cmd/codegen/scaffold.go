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

// scaffoldFrontend creates a minimal Vite + TypeScript frontend (React, the default) from templates.
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
	}
	for _, path := range required {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("missing %s — schemaf requires a Vite + TypeScript frontend built with npm: frontend/package.json, frontend/vite.config.ts, frontend/index.html and frontend/src/main.tsx or frontend/src/main.ts. See EXTEND.md › Frontend", path)
		}
	}
	entry, err := frontendEntry()
	if err != nil {
		return err
	}

	var missing []string
	for _, c := range runtimeConfigChecks(entry) {
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

// frontendEntries are the accepted entry files, in order of preference:
// src/main.tsx (the React scaffold) or src/main.ts (Vue, Svelte, plain TS).
var frontendEntries = []string{"frontend/src/main.tsx", "frontend/src/main.ts"}

// frontendEntry returns the frontend's entry file. Exactly one must exist:
// none is an error, and so are both (it's unclear which one index.html loads).
func frontendEntry() (string, error) {
	var found []string
	for _, p := range frontendEntries {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("missing the frontend entry file: frontend/src/main.tsx or frontend/src/main.ts — schemaf requires a Vite + TypeScript frontend. See EXTEND.md › Frontend")
	default:
		return "", fmt.Errorf("both frontend/src/main.tsx and frontend/src/main.ts exist — keep only the entry file index.html loads. See EXTEND.md › Frontend")
	}
}

type runtimeConfigCheck struct {
	path    string
	ok      *regexp.Regexp
	what    string
	snippet string
}

// runtimeConfigChecks are the pieces of the frontend that runtime config
// (config.gen.ts) relies on. entry is the frontend's entry file.
func runtimeConfigChecks(entry string) []runtimeConfigCheck {
	return []runtimeConfigCheck{
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
			path:    entry,
			ok:      regexp.MustCompile(`import\s+["']\./api/generated/config\.gen(\.ts)?["']`),
			what:    "add as the first import (loads runtime config before the app renders)",
			snippet: `import "./api/generated/config.gen";`,
		},
	}
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
