package codegen

import (
	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
)

// SubcommandProvider returns the codegen subcommand tree for use in a CLI.
func SubcommandProvider(ctx *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "codegen",
		Short: "Code generation utilities",
		Long:  `Generate code from your project's SQL queries, schemas, and API definitions.`,
	}

	cmd.AddCommand(newMigrationsCmd(ctx))
	cmd.AddCommand(newSQLCCmd(ctx))
	cmd.AddCommand(newEndpointsCmd(ctx))
	cmd.AddCommand(newAPITSCmd(ctx))
	cmd.AddCommand(newComposeCmd(ctx))
	cmd.AddCommand(newTestsCmd(ctx))
	cmd.AddCommand(newAllCmd(ctx))

	return []*cobra.Command{cmd}
}

func newAllCmd(ctx *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "all",
		Short: "Run all codegen steps",
		Long:  `Runs compose, migrations, sqlc, endpoints, apits, and tests codegen in order.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runComposeGen(); err != nil {
				return err
			}
			if err := runMigrationsGen(ctx); err != nil {
				return err
			}
			if err := runSQLC(ctx); err != nil {
				return err
			}
			if err := runEndpointsGen(ctx); err != nil {
				return err
			}
			if err := runAPITS(); err != nil {
				return err
			}
			return runTestsGen()
		},
	}
}
