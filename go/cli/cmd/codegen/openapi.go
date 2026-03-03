package codegen

import (
	"fmt"
	"io"
	"net/http"
	"os"

	cli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

func newOpenAPICmd(ctx *cli.Context) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "openapi <url>",
		Short: "Fetch /openapi.ts and write the TypeScript API client",
		Long: `Fetch the TypeScript client from <url>/openapi.ts and write it locally.

Example:
  atlas codegen openapi http://localhost:7001
  atlas codegen openapi http://localhost:7001 --output frontend/src/api.gen.ts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpenAPI(ctx, args[0], output)
		},
	}

	cmd.Flags().StringVar(&output, "output", "frontend/src/api.gen.ts", "Output file path")

	return cmd
}

func runOpenAPI(ctx *cli.Context, baseURL, outputPath string) error {
	tsURL := baseURL + "/openapi.ts"
	cli.Info("Fetching %s", tsURL)

	resp, err := http.Get(tsURL)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", tsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(parentDir(outputPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	if err := os.WriteFile(outputPath, body, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	cli.Success("Written %s (%d bytes)", outputPath, len(body))
	return nil
}

// parentDir returns the directory portion of a file path.
func parentDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
