package compose

import (
	"fmt"
	"os"

	cli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

func newBuildCmd(ctx *cli.Context) *cobra.Command {
	_ = ctx
	var outputPath string

	cmd := &cobra.Command{
		Use:   "build <compose-file>",
		Short: "Resolve and export a merged compose file",
		Long: `Resolve the dependency graph and export a single canonical merged compose file.

Uses 'docker compose config' to merge and interpolate all files.
Output goes to stdout by default (pipe or redirect as needed).

Examples:
  atlas compose build atlas-graph/compose/graph-api.yml
  atlas compose build atlas-graph/compose/graph-api.yml --output deploy/stack.yml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := resolveAndPrint(args[0])
			if err != nil {
				return err
			}

			setupEnv(files, ctx.HomeDir)

			composeArgs := buildDockerComposeArgs(files)
			composeArgs = append(composeArgs, "config", "--format", "yaml")

			merged, err := runDockerComposeCapture(composeArgs)
			if err != nil {
				return fmt.Errorf("docker compose config failed: %w", err)
			}

			if outputPath != "" {
				if err := os.WriteFile(outputPath, []byte(merged), 0644); err != nil {
					return fmt.Errorf("writing output file: %w", err)
				}
				cli.Success("Merged compose written to: %s", outputPath)
			} else {
				fmt.Print(merged)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write merged compose to file instead of stdout")

	return cmd
}
