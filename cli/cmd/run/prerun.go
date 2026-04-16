// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package run

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newPrerunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "prerun",
		Short:  "Stop conflicting dev services before production start",
		Long:   `Checks if dev compose services are running and stops them. Called by schemaf.sh before starting production compose.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDevIfRunning()
		},
	}
}

// stopDevIfRunning checks if dev compose services are running and stops them.
func stopDevIfRunning() error {
	compose := []string{"docker", "compose", "-f", "compose.gen.yml", "-f", "compose.dev.gen.yml"}

	// Check if any dev services are running.
	psArgs := append(compose, "ps", "-q")
	out, err := exec.Command(psArgs[0], psArgs[1:]...).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stderr, "Stopping dev services before production start...")
	downArgs := append(compose, "down")
	cmd := exec.Command(downArgs[0], downArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stopping dev services: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Dev services stopped.")
	return nil
}
