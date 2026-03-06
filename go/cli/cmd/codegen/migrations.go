// NOTE: This file is UNTESTED — new, unverified draft.

package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"text/template"

	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

//go:embed migrations.gen.go.tmpl
var migrationsGenTemplate string

func newMigrationsCmd(ctx *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "migrations",
		Short: "Generate go/db/migrations.gen.go",
		Long: `Scans go/db/migrations/*.sql and generates go/db/migrations.gen.go.

The generated file embeds all migration SQL files and exposes Provider()
for use in main.go: app.AddDb(db.Provider)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrationsGen(ctx)
		},
	}
}

func runMigrationsGen(_ *cli.Context) error {
	// Verify normative migrations directory exists
	if _, err := os.Stat(migrationsDir); err != nil {
		return fmt.Errorf("migrations directory %q not found (run from project root): %w", migrationsDir, err)
	}

	// Check there is at least one .sql file to embed
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", migrationsDir, err)
	}
	hasSQLFiles := false
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".sql" {
			hasSQLFiles = true
			break
		}
	}
	if !hasSQLFiles {
		cli.Warning("no .sql files found in %s — skipping migrations.gen.go", migrationsDir)
		return nil
	}

	tmpl, err := template.New("migrations").Parse(migrationsGenTemplate)
	if err != nil {
		return fmt.Errorf("parsing migrations.gen.go template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return fmt.Errorf("executing migrations.gen.go template: %w", err)
	}

	const outPath = dbDir + "/migrations.gen.go"
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	cli.Success("Generated %s", outPath)
	return nil
}
