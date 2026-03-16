// This is a CLI built from the extensible, modular CLI kit provided by the framwork. Users will
// build their own CLI extending(!) the built in features - so the users project CLI is an *extended*
// version of this CLI, not a seperate CLI. The single CLI will be used for framwork operations like
// codegen and running the server, but also for user defined commands. The user will write his own main.go
// to mount all subcommands and call Execute().
package main

import (
	"log"
	"os"

	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/cli/cmd/codegen"
	"github.com/flocko-motion/schemaf/cli/cmd/run"
)

func main() {
	c, err := cli.New()
	if err != nil {
		log.Fatalf("init cli: %v", err)
	}

	c.AddSubcommands(codegen.SubcommandProvider)
	c.AddSubcommands(run.SubcommandProvider)

	if err := c.Execute(); err != nil {
		os.Exit(1)
	}
}
