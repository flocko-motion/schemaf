// schemaf is the standalone framework CLI used by codegen.sh.
// It has no knowledge of any specific project — it reads project files from disk.
// Projects invoke it via: go run github.com/yourorg/schemaf/cmd/schemaf <command>
package main

import (
	"log"
	"os"

	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/cli/cmd/codegen"
	"github.com/flocko-motion/schemaf/cli/cmd/run"
)


func main() {
	// Change to the project root (directory containing schemaf.toml) so all
	// commands can use paths relative to the project root regardless of where
	// the script was invoked from.
	if root, err := cli.FindProjectRoot(); err == nil {
		if err := os.Chdir(root); err != nil {
			log.Fatalf("chdir to project root: %v", err)
		}
	}

	// Derive project home from schemaf.toml; fall back to ~/.schemaf for non-project contexts.
	projectHome := func() string {
		name, err := cli.ReadProjectName()
		if err != nil {
			home, _ := os.UserHomeDir()
			return home + "/.schemaf"
		}
		return cli.ProjectHome(name)
	}()

	c, err := cli.New(projectHome)
	if err != nil {
		log.Fatalf("init cli: %v", err)
	}

	c.AddSubcommands(codegen.SubcommandProvider)
	c.AddSubcommands(run.SubcommandProvider)

	if err := c.Execute(); err != nil {
		os.Exit(1)
	}
}
