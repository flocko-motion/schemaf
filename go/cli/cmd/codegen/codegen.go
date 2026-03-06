package codegen

import (
	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

// SubcommandProvider returns the codegen subcommand tree for use in a CLI.
func SubcommandProvider(ctx *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "codegen",
		Short: "Code generation utilities",
		Long:  `Generate code from your project's SQL queries, schemas, and API definitions.`,
	}

	cmd.AddCommand(newSQLCCmd(ctx))
	cmd.AddCommand(newOpenAPICmd(ctx))
	cmd.AddCommand(newComposeCmd(ctx))

	return []*cobra.Command{cmd}
}
