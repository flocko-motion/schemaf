// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package run

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
)

// SubcommandProvider returns top-level run commands (test, etc.).
func SubcommandProvider(_ *cli.Context) []*cobra.Command {
	return []*cobra.Command{newTestCmd(), newPrerunCmd()}
}

func newTestCmd() *cobra.Command {
	var verbose, noCache bool

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run project tests",
		Long:  `Runs Go tests across ./go/... and TypeScript type-check in ./frontend.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTests(verbose, noCache)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose go test output")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass go test cache (-count=1)")

	return cmd
}

func runTests(verbose, noCache bool) error {
	if err := runGoTests(verbose, noCache); err != nil {
		return err
	}
	tscCmd := exec.Command("npx", "tsc", "--noEmit")
	tscCmd.Dir = "frontend"
	tscCmd.Stdout = os.Stdout
	tscCmd.Stderr = os.Stderr
	return tscCmd.Run()
}

func runGoTests(verbose, noCache bool) error {
	// Run both project tests (./go/...) and framework tests (github.com/flocko-motion/schemaf/...).
	// Framework tests validate guarantees (migration ordering, DB reset filter, etc.)
	// that all projects depend on.
	pkgs := []string{"./go/...", "github.com/flocko-motion/schemaf/..."}

	// Use gotestsum for pretty output when available, fall back to go test.
	if path, err := exec.LookPath("gotestsum"); err == nil {
		args := []string{path, "--format", "testdox"}
		if noCache {
			args = append(args, "--rerun-fails=0", "--")
			args = append(args, "-count=1")
		} else {
			args = append(args, "--")
		}
		args = append(args, pkgs...)
		if verbose {
			args = append(args, "-v")
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	fmt.Fprintln(os.Stderr, "WARNING: gotestsum not found — output will be plain go test.")
	fmt.Fprintln(os.Stderr, "         Install with: go install gotest.tools/gotestsum@latest")

	args := []string{"test"}
	args = append(args, pkgs...)
	if verbose {
		args = append(args, "-v")
	}
	if noCache {
		args = append(args, "-count=1")
	}
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
