package ctl

import (
	cli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

func newStopCmd(ctx *cli.Context) *cobra.Command {
	_ = ctx
	cmd := &cobra.Command{
		Use:   "stop <compose-file>",
		Short: "Stop all services in a composition",
		Long: `Resolve the dependency graph and stop all services.

Example:
  zeus ctl stop example/compose/app.yml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := resolveAndPrint(args[0])
			if err != nil {
				return err
			}

			composeArgs := buildDockerComposeArgs(files)
			composeArgs = append(composeArgs, "down")

			if err := runDockerCompose(composeArgs); err != nil {
				return err
			}

			cli.Success("Stopped all services")
			return nil
		},
	}

	return cmd
}
