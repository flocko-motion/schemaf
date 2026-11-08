// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package run

import (
	"fmt"

	"github.com/spf13/cobra"

	cli "github.com/flocko-motion/schemaf/cli"
)

func newPrerunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prerun test <project-name>",
		Short: "Check the container runtime before schemaf.sh starts the test stack",
		Long: `Called by schemaf.sh before it starts containers for "test".

Checks the selected container runtime (SCHEMAF_RUNTIME) and its compose, and
refuses to start if the other runtime holds the project's Postgres volume.
("build" and "run" do the same checks in schemaf.sh itself, so they need no Go.)`,
		Hidden:       true,
		SilenceUsage: true, // errors here are runtime problems, not usage mistakes
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, project := args[0], args[1]
			if mode != "test" {
				return fmt.Errorf("prerun: unknown mode %q (want test)", mode)
			}
			return cli.CheckRuntime(project)
		},
	}
}
