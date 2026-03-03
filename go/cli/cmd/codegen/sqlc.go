package codegen

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	cli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

//go:embed sqlc.yaml.tmpl
var sqlcYAMLTemplate string

func newSQLCCmd(ctx *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "sqlc",
		Short: "Generate sqlc.yaml and run sqlc generate",
		Long: `Generate db/sqlc.yaml from the normative template and run sqlc generate.

Expects db/queries/ and db/migrations/ to exist in the current directory.
The Go package name is inferred from the nearest go.mod file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSQLC(ctx)
		},
	}
}

func runSQLC(ctx *cli.Context) error {
	// Verify required directories exist
	for _, dir := range []string{"db/queries", "db/migrations"} {
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("required directory %q not found (run from project root): %w", dir, err)
		}
	}

	// Infer Go package name from go.mod
	pkg, err := inferPackageName()
	if err != nil {
		cli.Warning("could not infer package name from go.mod, using 'db': %v", err)
		pkg = "db"
	}

	// Generate sqlc.yaml from template
	tmpl, err := template.New("sqlc").Parse(sqlcYAMLTemplate)
	if err != nil {
		return fmt.Errorf("parsing sqlc.yaml template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"Package": pkg}); err != nil {
		return fmt.Errorf("executing sqlc.yaml template: %w", err)
	}

	const sqlcYAMLPath = "db/sqlc.yaml"
	if err := os.WriteFile(sqlcYAMLPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", sqlcYAMLPath, err)
	}
	cli.Info("Generated %s", sqlcYAMLPath)

	// Run sqlc generate
	sqlcCmd := exec.Command("sqlc", "generate", "--file", sqlcYAMLPath)
	sqlcCmd.Stdout = os.Stdout
	sqlcCmd.Stderr = os.Stderr
	if err := sqlcCmd.Run(); err != nil {
		return fmt.Errorf("sqlc generate failed: %w", err)
	}

	cli.Success("sqlc generate completed")
	return nil
}

// inferPackageName reads the module name from go.mod and returns the last path segment.
func inferPackageName() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			module := strings.TrimPrefix(line, "module ")
			module = strings.TrimSpace(module)
			parts := strings.Split(module, "/")
			return parts[len(parts)-1], nil
		}
	}
	return "", fmt.Errorf("module declaration not found in go.mod")
}
