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

//go:embed frontend.gen.go.tmpl
var frontendGenTemplate string

func newFrontendCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "frontend",
		Short: "Generate go/frontend.gen.go",
		Long:  `Generates go/frontend.gen.go with embedded frontend assets for production serving.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFrontendGen()
		},
	}
}

func runFrontendGen() error {
	// Only generate if project has a frontend/ directory.
	if _, err := os.Stat("frontend"); os.IsNotExist(err) {
		return nil
	}

	const outPath = "go/frontend.gen.go"
	if err := renderTemplate(frontendGenTemplate, outPath, nil); err != nil {
		return fmt.Errorf("generating frontend: %w", err)
	}

	// Ensure go/frontend_dist/ exists with a .gitkeep so //go:embed resolves.
	// In production, the Dockerfile populates this with real assets.
	if err := os.MkdirAll("go/frontend_dist", 0755); err != nil {
		return fmt.Errorf("creating frontend_dist: %w", err)
	}
	gitkeep := "go/frontend_dist/.gitkeep"
	if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
		if err := os.WriteFile(gitkeep, nil, 0644); err != nil {
			return fmt.Errorf("creating .gitkeep: %w", err)
		}
	}

	// Ensure .gitignore excludes built assets but keeps .gitkeep.
	if err := ensureGitignore("go/.gitignore", "frontend.gen.go"); err != nil {
		return err
	}
	if err := ensureGitignore("go/.gitignore", "frontend_dist/"); err != nil {
		return err
	}
	if err := ensureGitignore("go/.gitignore", "!frontend_dist/.gitkeep"); err != nil {
		return err
	}

	cli.Success("Generated %s", outPath)
	return nil
}
