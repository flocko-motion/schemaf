// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
)

func newAPITSCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "apits",
		Short: "Generate TypeScript API client from openapi.json",
		Long:  `Runs npx swagger-typescript-api generate on openapi.json to produce frontend/src/api/generated/api.gen.ts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPITS()
		},
	}
}

func runAPITS() error {
	if _, err := os.Stat("gen/openapi.json"); os.IsNotExist(err) {
		cli.Warning("gen/openapi.json not found — skipping TypeScript client generation")
		return nil
	}
	cmd := exec.Command("npx", "--yes", "swagger-typescript-api", "generate",
		"-p", "gen/openapi.json",
		"-o", "frontend/src/api/generated",
		"--name", "api.gen.ts",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("swagger-typescript-api: %w", err)
	}
	cli.Success("Generated frontend/src/api/generated/api.gen.ts")
	return nil
}
