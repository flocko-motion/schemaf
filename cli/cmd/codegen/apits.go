// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"os"
	"os/exec"

	cli "github.com/flocko-motion/schemaf/cli"
)

// The TypeScript client generator, pinned to an exact version: an unpinned npx
// fetches whatever is newest, so the same openapi.json could produce a
// different api.gen.ts from one day to the next. Requires Node >= 20.
const (
	swaggerTSAPIPkg     = "swagger-typescript-api"
	swaggerTSAPIVersion = "13.14.0"
)

// apitsArgs are the npx arguments that generate api.gen.ts with the pinned generator.
func apitsArgs() []string {
	return []string{"--yes", swaggerTSAPIPkg + "@" + swaggerTSAPIVersion, "generate",
		"-p", "gen/openapi.json",
		"-o", "frontend/src/api/generated",
		"--name", "api.gen.ts",
		// Enums as unions of their values (type S = "a" | "b"), not TS
		// enums: callers pass plain literals, and nothing exists at runtime.
		"--enum-style=union",
	}
}

func runAPITS() error {
	if _, err := os.Stat("gen/openapi.json"); os.IsNotExist(err) {
		cli.Warning("gen/openapi.json not found — skipping TypeScript client generation")
		return nil
	}
	cmd := exec.Command("npx", apitsArgs()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("swagger-typescript-api: %w", err)
	}
	cli.Success("Generated frontend/src/api/generated/api.gen.ts")
	return nil
}
