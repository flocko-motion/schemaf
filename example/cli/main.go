package main

import (
	"fmt"
	"log"
	"os"

	basecli "atlas.local/base/cli"
	"atlas.local/base/cli/cmd/codegen"
	"atlas.local/base/cli/cmd/compose"
	"atlas.local/example/cli/cmd"
)

func main() {
	homeDir := os.Getenv("ATLAS_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("HOME") + "/.atlas"
	}

	c, err := basecli.New(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	c.AddSubcommands(
		compose.SubcommandProvider,
		codegen.SubcommandProvider,
		cmd.TodoSubcommandProvider,
	)

	if err := c.Execute(); err != nil {
		log.Fatal(err)
	}
}
