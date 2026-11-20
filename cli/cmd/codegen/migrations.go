// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed migrations.gen.go.tmpl
var migrationsGenTemplate string

func runMigrationsGen(_ *cli.Context) error {
	// Ensure normative migrations directory exists
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", migrationsDir, err)
	}

	hasMigrations, err := hasSQLFiles(migrationsDir)
	if err != nil {
		return err
	}
	if !hasMigrations {
		warnEmptyInput("no migrations in %s — db.Provider() is empty", migrationsDir)
	}

	src, err := renderMigrationsGen(hasMigrations)
	if err != nil {
		return err
	}
	const outPath = dbDir + "/migrations.gen.go"
	if err := os.WriteFile(outPath, src, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	cli.Success("Generated %s", outPath)
	return nil
}

// renderMigrationsGen renders go/db/migrations.gen.go. Without migrations it
// has no //go:embed line (a pattern matching nothing doesn't compile) and
// Provider() returns an empty embed.FS. See EXTEND.md › Empty inputs.
func renderMigrationsGen(hasMigrations bool) ([]byte, error) {
	tmpl, err := template.New("migrations").Parse(migrationsGenTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing migrations.gen.go template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"HasMigrations": hasMigrations}); err != nil {
		return nil, fmt.Errorf("executing migrations.gen.go template: %w", err)
	}
	return buf.Bytes(), nil
}

// hasSQLFiles reports whether dir holds at least one *.sql file.
func hasSQLFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			return true, nil
		}
	}
	return false, nil
}
