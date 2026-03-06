package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

//go:embed compose.gen.yml.tmpl
var composeGenTemplate string

//go:embed compose.dev.yml.tmpl
var composeDevTemplate string

func newComposeCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "compose",
		Short: "Generate compose.gen.yml",
		Long: `Generates compose.gen.yml from the framework template.

Includes postgres + backend services. Any *.yml files in compose/ are
included via Docker Compose include directives.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runComposeGen()
		},
	}
}

func runComposeGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}

	extensions, err := scanComposeExtensions()
	if err != nil {
		return err
	}

	data := map[string]any{"Name": name, "Extensions": extensions}

	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}

	if err := renderTemplate(composeGenTemplate, "gen/compose.gen.yml", data); err != nil {
		return err
	}
	cli.Success("Generated gen/compose.gen.yml (project: %s)", name)

	if err := renderTemplate(composeDevTemplate, "gen/compose.dev.yml", data); err != nil {
		return err
	}
	cli.Success("Generated gen/compose.dev.yml")
	return nil
}

func renderTemplate(tmplStr, outPath string, data map[string]any) error {
	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", outPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template %s: %w", outPath, err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}
	return nil
}

// readProjectName reads the `name` field from schemaf.toml.
func readProjectName() (string, error) {
	return cli.ReadProjectName()
}

// scanComposeExtensions returns relative paths to *.yml files in compose/.
func scanComposeExtensions() ([]string, error) {
	entries, err := os.ReadDir("compose")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading compose/: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yml" {
			paths = append(paths, "compose/"+e.Name())
		}
	}
	return paths, nil
}
