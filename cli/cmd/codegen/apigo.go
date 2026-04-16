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

func newAPIGoCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "apigo",
		Short: "Generate Go API client from openapi.json",
		Long:  `Runs oapi-codegen on gen/openapi.json to produce go/apiclient/client.gen.go.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIGo()
		},
	}
}

func runAPIGo() error {
	if _, err := os.Stat("gen/openapi.json"); os.IsNotExist(err) {
		cli.Warning("gen/openapi.json not found — skipping Go client generation")
		return nil
	}

	if err := os.MkdirAll("go/apiclient", 0755); err != nil {
		return fmt.Errorf("creating go/apiclient/: %w", err)
	}

	cmd := exec.Command("go", "run",
		"github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen",
		"--package", "apiclient",
		"--generate", "types,client",
		"-o", "apiclient/client.gen.go",
		"../gen/openapi.json",
	)
	cmd.Dir = "go"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oapi-codegen: %w", err)
	}
	cli.Success("Generated go/apiclient/client.gen.go")
	return nil
}
