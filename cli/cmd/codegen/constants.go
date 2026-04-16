// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	_ "embed"
	"fmt"

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
	port, err := readPort()
	if err != nil {
		return err
	}

	data := map[string]any{"Name": name, "Port": port}

	const outPath = "go/constants.gen.go"
	if err := renderTemplate(constantsGenTemplate, outPath, data); err != nil {
		return fmt.Errorf("generating constants: %w", err)
	}

	cli.Success("Generated %s (project: %s, port: %d)", outPath, name, port)
	return nil
}
