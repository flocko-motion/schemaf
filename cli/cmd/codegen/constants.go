// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	_ "embed"
	"fmt"
	"os"

	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed constants.gen.go.tmpl
var constantsGenTemplate string

func runConstantsGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}
	port, err := readPort()
	if err != nil {
		return err
	}

	data := map[string]any{"Name": name, "Port": port}

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", appDir, err)
	}
	const outPath = "go/app/constants.gen.go"
	if err := renderTemplate(constantsGenTemplate, outPath, data); err != nil {
		return fmt.Errorf("generating constants: %w", err)
	}
	if err := removeLegacyAppFiles(); err != nil {
		return err
	}

	cli.Success("Generated %s (project: %s, port: %d)", outPath, name, port)
	return nil
}
