// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed frontend.gen.go.tmpl
var frontendGenTemplate string

//go:embed config.gen.ts.tmpl
var configGenTS string

// configGenTSOut is the generated runtime config reader (see EXTEND.md, Runtime config).
const configGenTSOut = "frontend/src/api/generated/config.gen.ts"

//go:embed version.gen.ts.tmpl
var versionGenTS string

// versionGenTSOut is the generated version check (see EXTEND.md, Noticing an
// upgrade in an open page).
const versionGenTSOut = "frontend/src/api/generated/version.gen.ts"

func runFrontendGen() error {
	// Only generate if project has a frontend/ directory.
	if _, err := os.Stat("frontend"); os.IsNotExist(err) {
		return nil
	}

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", appDir, err)
	}
	const outPath = "go/app/frontend.gen.go"
	if err := renderTemplate(frontendGenTemplate, outPath, nil); err != nil {
		return fmt.Errorf("generating frontend: %w", err)
	}

	// Ensure go/app/frontend_dist/ exists with a .gitkeep so //go:embed resolves.
	// This is the one deliberate placeholder file (EXTEND.md › Empty inputs):
	// every other empty input selects a template variant instead, but this
	// directory is filled by the frontend build at image build time
	// (Dockerfile.gen), so a variant chosen now would be wrong for the image.
	if err := os.MkdirAll("go/app/frontend_dist", 0755); err != nil {
		return fmt.Errorf("creating frontend_dist: %w", err)
	}
	gitkeep := "go/app/frontend_dist/.gitkeep"
	if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
		if err := os.WriteFile(gitkeep, nil, 0644); err != nil {
			return fmt.Errorf("creating .gitkeep: %w", err)
		}
	}
	if err := removeLegacyAppFiles(); err != nil {
		return err
	}

	cli.Success("Generated %s", outPath)

	for _, f := range []struct{ path, content string }{
		{configGenTSOut, configGenTS},
		{versionGenTSOut, versionGenTS},
	} {
		if err := writeStaticTS(f.path, f.content); err != nil {
			return err
		}
		cli.Success("Generated %s", f.path)
	}
	return nil
}

// writeStaticTS writes a generated TypeScript file whose content is the same
// for every project (the runtime config reader, the version check).
func writeStaticTS(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
