// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package ctl

import (
	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
)

func newDevCmd(ctx *cli.Context) *cobra.Command {
	var nativeMode string
	var skipBuild bool
	var wait bool

	cmd := &cobra.Command{
		Use:   "dev <compose-file> <services>",
		Short: "Run only selected services in Docker",
		Long: `Resolve the dependency graph and run only a subset of services in Docker.

Services should be provided as a comma-separated list of short names.

Example:
  schemaf ctl dev example/compose/app.yml db,backend`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompose(ctx, args[0], args[1], nativeMode, skipBuild, wait)
		},
	}

	cmd.Flags().StringVar(&nativeMode, "native", "", "Stop container and run this service natively (prints dev-instructions)")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Start without rebuilding containers")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for services to be healthy before returning")

	return cmd
}
