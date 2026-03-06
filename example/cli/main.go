package main

import (
	"fmt"
	"log"
	"os"

	basecli "schemaf.local/base/cli"
	"schemaf.local/base/cli/cmd/codegen"
	"schemaf.local/base/cli/cmd/ctl"
	"schemaf.local/example/cli/cmd"
)

// REFERENCE IMPLEMENTATION (README design):
// In the README design, the CLI and backend are unified - they're the same binary.
// The backend/main.go would call schemaf.New(ctx) which provides:
// - server command
// - dev command
// - compose commands
// - codegen command
// All built-in, with optional subcommands added via app.AddSubcommand()
//
// This separate CLI is temporary during migration.
// Future: Remove example/cli/, merge functionality into backend/main.go

func main() {
	homeDir := os.Getenv("SCHEMAF_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("HOME") + "/.schemaf"
	}

	c, err := basecli.New(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// TODO(framework): These should be built-in to schemaf.New(ctx)
	// README design: app := schemaf.New(ctx) already includes compose, codegen, server, dev
	c.AddSubcommands(
		ctl.SubcommandProvider,     // TODO: rename ctl -> compose (README uses "compose")
		codegen.SubcommandProvider, // TODO: merge into framework
		cmd.TodoSubcommandProvider, // Example of project-specific subcommand (this stays)
	)

	if err := c.Execute(); err != nil {
		log.Fatal(err)
	}
}
