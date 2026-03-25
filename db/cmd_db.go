// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Command returns the "db" cobra command group with query and migrate subcommands.
func Command() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations",
	}

	var jsonOutput bool

	queryCmd := &cobra.Command{
		Use:   "query [sql]",
		Short: "Execute arbitrary SQL against the project database",
		Long:  "Runs a SQL statement and prints the results as a table (default) or JSON (--json).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sql := args[0]
			d := DB()
			if d == nil {
				return fmt.Errorf("database not available — is postgres running?")
			}

			rows, err := d.QueryContext(cmd.Context(), sql)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			defer rows.Close()

			cols, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("reading columns: %w", err)
			}

			if jsonOutput {
				return printJSON(rows, cols)
			}
			return printTable(rows, cols)
		},
	}
	queryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := DB()
			if d == nil {
				return fmt.Errorf("database not available — is postgres running?")
			}
			return RunMigrations(context.Background())
		},
	}

	dbCmd.AddCommand(queryCmd, migrateCmd, backupCmd(), restoreCmd())
	return dbCmd
}

func printTable(rows interface {
	Next() bool
	Scan(dest ...any) error
}, cols []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(cols, "\t"))
	fmt.Fprintln(w, strings.Repeat("─\t", len(cols)))

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	count := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scanning row: %w", err)
		}
		parts := make([]string, len(cols))
		for i, v := range vals {
			parts[i] = fmt.Sprintf("%v", v)
		}
		fmt.Fprintln(w, strings.Join(parts, "\t"))
		count++
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "(%d rows)\n", count)
	return nil
}

func printJSON(rows interface {
	Next() bool
	Scan(dest ...any) error
}, cols []string) error {
	var results []map[string]any

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scanning row: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			switch v := vals[i].(type) {
			case []byte:
				row[col] = string(v)
			default:
				row[col] = v
			}
		}
		results = append(results, row)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}
