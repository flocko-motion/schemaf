// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package initialize

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed templates/*
var templates embed.FS

// SubcommandProvider returns the init command.
func SubcommandProvider(_ *cli.Context) []*cobra.Command {
	return []*cobra.Command{newInitCmd()}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new schemaf project",
		Long: `Creates a new schemaf project directory with a minimal working app:

  schemaf init myapp

This creates myapp/ with a complete project structure, ready for
./schemaf.sh codegen && ./schemaf.sh dev all`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(args[0])
		},
	}
}

func runInit(name string) error {
	// Validate name.
	if strings.ContainsAny(name, " /\\") {
		return fmt.Errorf("project name %q must not contain spaces or slashes", name)
	}

	// Check directory doesn't exist.
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	title := strings.ReplaceAll(strings.Title(strings.ReplaceAll(name, "-", " ")), " ", " ")
	goVersion := runtime.Version()
	if strings.HasPrefix(goVersion, "go") {
		goVersion = goVersion[2:]
	}
	data := map[string]any{
		"Name":      name,
		"Title":     title,
		"GoVersion": goVersion,
	}

	// Create directory structure.
	dirs := []string{
		"go/api",
		"go/db/migrations",
		"go/db/queries",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(name, d), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}

	// Render templates.
	files := []struct {
		tmpl string
		path string
	}{
		{"templates/schemaf.toml.tmpl", "schemaf.toml"},
		{"templates/go.work.tmpl", "go.work"},
		{"templates/main.go.tmpl", "go/main.go"},
		{"templates/number.go.tmpl", "go/api/number.go"},
		{"templates/0001_number.sql.tmpl", "go/db/migrations/0001_number.sql"},
		{"templates/number.sql.tmpl", "go/db/queries/number.sql"},
	}

	for _, f := range files {
		if err := renderFile(f.tmpl, filepath.Join(name, f.path), data); err != nil {
			return fmt.Errorf("writing %s: %w", f.path, err)
		}
	}

	// Initialize Go module.
	cli.Info("Initializing Go module...")
	goDir := filepath.Join(name, "go")
	if err := run(goDir, "go", "mod", "init", "schemaf.local/"+name); err != nil {
		return fmt.Errorf("go mod init: %w", err)
	}
	if err := run(goDir, "go", "get", "github.com/flocko-motion/schemaf@latest"); err != nil {
		return fmt.Errorf("go get schemaf: %w", err)
	}

	// Run codegen to generate schemaf.sh, compose files, frontend scaffold, etc.
	cli.Info("Running codegen...")
	if err := run(name, "go", "run", "github.com/flocko-motion/schemaf/cmd/schemaf", "codegen", "all"); err != nil {
		return fmt.Errorf("codegen: %w", err)
	}

	// Replace the scaffolded App.tsx with the number app.
	if err := writeAppTSX(filepath.Join(name, "frontend/src/App.tsx"), title); err != nil {
		return fmt.Errorf("writing App.tsx: %w", err)
	}

	cli.Success("Project %q created!", name)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  cd %s\n", name)
	fmt.Fprintln(os.Stderr, "  ./schemaf.sh dev all")
	fmt.Fprintln(os.Stderr)

	return nil
}

func writeAppTSX(outPath, title string) error {
	content := `import { useState, useEffect } from "react";

export default function App() {
  const [number, setNumber] = useState(0);
  const [input, setInput] = useState("");

  useEffect(() => {
    fetch("/api/number")
      .then((r) => r.json())
      .then((d) => {
        setNumber(d.number);
        setInput(String(d.number));
      });
  }, []);

  const save = () => {
    fetch("/api/number", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ number: parseInt(input) || 0 }),
    })
      .then((r) => r.json())
      .then((d) => setNumber(d.number));
  };

  return (
    <div style={{ padding: "2rem", fontFamily: "system-ui" }}>
      <h1>` + title + `</h1>
      <p>Current number: <strong>{number}</strong></p>
      <input
        type="number"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && save()}
      />
      <button onClick={save}>Save</button>
    </div>
  );
}
`
	return os.WriteFile(outPath, []byte(content), 0644)
}

func renderFile(tmplPath, outPath string, data map[string]any) error {
	content, err := templates.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("reading template %s: %w", tmplPath, err)
	}
	tmpl, err := template.New("").Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", tmplPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template %s: %w", tmplPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outPath, buf.Bytes(), 0644)
}

func run(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
