// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package run

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cli "github.com/flocko-motion/schemaf/cli"
)

func newPrerunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prerun <run|test> <project-name>",
		Short: "Check the container runtime before schemaf.sh starts containers",
		Long: `Called by schemaf.sh before it starts containers for "run" or "test".

Both: checks the selected container runtime (SCHEMAF_RUNTIME) and its compose,
and refuses to start if the other runtime holds the project's Postgres volume.
"run" additionally stops dev compose services, which can't coexist with production.`,
		Hidden:       true,
		SilenceUsage: true, // errors here are runtime problems, not usage mistakes
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, project := args[0], args[1]
			if mode != "run" && mode != "test" {
				return fmt.Errorf("prerun: unknown mode %q (want run or test)", mode)
			}
			if err := cli.CheckRuntime(project); err != nil {
				return err
			}
			if mode == "run" {
				return stopDevIfRunning()
			}
			return nil
		},
	}
}

// stopDevIfRunning checks if dev compose services are running and stops them.
func stopDevIfRunning() error {
	compose := []string{"compose", "-f", "compose.gen.yml", "-f", "compose.dev.gen.yml"}

	// Check if any dev services are running.
	ps, err := cli.Container(append(compose, "ps", "-q")...)
	if err != nil {
		return err
	}
	out, err := ps.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stderr, "Stopping dev services before production start...")
	down, err := cli.Container(append(compose, "down")...)
	if err != nil {
		return err
	}
	down.Stdout = os.Stdout
	down.Stderr = os.Stderr
	if err := down.Run(); err != nil {
		return fmt.Errorf("stopping dev services: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Dev services stopped.")
	return nil
}
