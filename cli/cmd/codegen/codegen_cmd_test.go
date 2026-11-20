package codegen

import (
	"testing"

	cli "github.com/flocko-motion/schemaf/cli"
)

// codegen is one command that runs every step; the only subcommand left is
// the hidden `all`, the old spelling earlier schemaf.sh scripts call
// (EXTEND.md › Code Generation).
func TestCodegenIsOneCommand(t *testing.T) {
	cmds := SubcommandProvider(cli.NewTestContext())
	if len(cmds) != 1 || cmds[0].Name() != "codegen" {
		t.Fatalf("SubcommandProvider = %v, want just codegen", cmds)
	}
	cg := cmds[0]
	if cg.RunE == nil {
		t.Error("codegen itself doesn't run")
	}
	subs := cg.Commands()
	if len(subs) != 1 || subs[0].Name() != "all" || !subs[0].Hidden {
		var names []string
		for _, c := range subs {
			names = append(names, c.Name())
		}
		t.Fatalf("codegen subcommands = %v, want only the hidden all", names)
	}
}
