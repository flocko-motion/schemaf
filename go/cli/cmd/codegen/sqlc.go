// NOTE: This file has been updated but is UNTESTED. The sqlc invocation via
// embedded library (pkg/cli) and the normative path changes (go/db/) are new.

package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"text/template"

	sqllcpkg "github.com/sqlc-dev/sqlc/pkg/cli"
	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

//go:embed sqlc.yaml.tmpl
var sqlcYAMLTemplate string

// Normative paths — not configurable.
const (
	dbDir         = "go/db"
	queriesDir    = dbDir + "/queries"
	migrationsDir = dbDir + "/migrations"
	sqlcYAMLPath  = dbDir + "/sqlc.yaml"
)

func newSQLCCmd(ctx *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "sqlc",
		Short: "Generate sqlc.yaml and run sqlc generate",
		Long: `Generate go/db/sqlc.yaml from the normative template and run sqlc generate.

Expects go/db/queries/ and go/db/migrations/ to exist in the project root.
sqlc runs embedded — no external sqlc binary required.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSQLC(ctx)
		},
	}
}

func runSQLC(_ *cli.Context) error {
	// Verify normative directories exist
	for _, dir := range []string{queriesDir, migrationsDir} {
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("required directory %q not found (run from project root): %w", dir, err)
		}
	}

	// Generate sqlc.yaml — package is always "db", output always goes to go/db/
	tmpl, err := template.New("sqlc").Parse(sqlcYAMLTemplate)
	if err != nil {
		return fmt.Errorf("parsing sqlc.yaml template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Package": "db"}); err != nil {
		return fmt.Errorf("executing sqlc.yaml template: %w", err)
	}

	if err := os.WriteFile(sqlcYAMLPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", sqlcYAMLPath, err)
	}
	cli.Info("Generated %s", sqlcYAMLPath)

	// Run sqlc via embedded library — no external binary needed.
	// NOTE: sqlc resolves paths relative to the config file location (go/db/).
	if code := sqllcpkg.Run([]string{"generate", "--file", sqlcYAMLPath}); code != 0 {
		return fmt.Errorf("sqlc generate failed with exit code %d", code)
	}

	cli.Success("sqlc generate completed → go/db/queries.gen.go")
	return nil
}
