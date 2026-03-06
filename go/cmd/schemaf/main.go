// schemaf is the standalone framework CLI used by codegen.sh.
// It has no knowledge of any specific project — it reads project files from disk.
// Projects invoke it via: go run github.com/yourorg/schemaf/cmd/schemaf <command>
package main

import (
	"log"
	"os"

	"schemaf.local/base/cli"
	"schemaf.local/base/cli/cmd/codegen"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("getting home dir: %v", err)
	}

	c, err := cli.New(homeDir)
	if err != nil {
		log.Fatalf("init cli: %v", err)
	}

	c.AddSubcommands(codegen.SubcommandProvider)

	if err := c.Execute(); err != nil {
		os.Exit(1)
	}
}
