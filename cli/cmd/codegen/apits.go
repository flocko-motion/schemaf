// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"os"
	"os/exec"

	cli "github.com/flocko-motion/schemaf/cli"
)

func runAPITS() error {
	if _, err := os.Stat("gen/openapi.json"); os.IsNotExist(err) {
		cli.Warning("gen/openapi.json not found — skipping TypeScript client generation")
		return nil
	}
	cmd := exec.Command("npx", "--yes", "swagger-typescript-api", "generate",
		"-p", "gen/openapi.json",
		"-o", "frontend/src/api/generated",
		"--name", "api.gen.ts",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("swagger-typescript-api: %w", err)
	}
	cli.Success("Generated frontend/src/api/generated/api.gen.ts")
	return nil
}
