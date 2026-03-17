// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	_ "embed"
	"fmt"
	"os"

	cli "github.com/flocko-motion/schemaf/cli"
	"github.com/spf13/cobra"
)

//go:embed constants.gen.go.tmpl
var constantsGenTemplate string

func newConstantsCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "constants",
		Short: "Generate go/constants.gen.go",
		Long: `Generates go/constants.gen.go with the project name from schemaf.toml.

The generated file provides ProjectName for use in main.go: schemaf.New(ctx, ProjectName)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConstantsGen()
		},
	}
}

func runConstantsGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}

	data := map[string]any{"Name": name}

	const outPath = "go/constants.gen.go"
	if err := renderTemplate(constantsGenTemplate, outPath, data); err != nil {
		return fmt.Errorf("generating constants: %w", err)
	}

	// Ensure .gitignore includes generated file
	if err := ensureGitignore("go/.gitignore", "constants.gen.go"); err != nil {
		return err
	}

	cli.Success("Generated %s (project: %s)", outPath, name)
	return nil
}

// ensureGitignore adds entry to a .gitignore if not already present.
func ensureGitignore(path, entry string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	for _, line := range splitLines(string(content)) {
		if line == entry {
			return nil
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	prefix := ""
	if len(content) > 0 && content[len(content)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := fmt.Fprintf(f, "%s%s\n", prefix, entry); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
