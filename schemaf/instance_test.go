package schemaf

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/constants"
)

func TestResolveInstance(t *testing.T) {
	old := constants.Instance()
	t.Cleanup(func() { _ = constants.SetInstance(old) })
	if !constants.IsProjectNameSet() {
		constants.SetProjectName("myapp")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".schemaf"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".schemaf", "instance"), []byte("349\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{Use: "query"}

	// Production ignores the checkout's instance and refuses the flag.
	t.Setenv("SCHEMAF_DEV", "")
	a := &App{project: "myapp", projectRoot: root}
	if err := a.resolveInstance(cmd); err != nil || constants.Instance() != 0 {
		t.Fatalf("production: instance %d, %v", constants.Instance(), err)
	}
	a.instanceFlag = "349"
	if err := a.resolveInstance(cmd); err == nil || !strings.Contains(err.Error(), "dev-only") {
		t.Fatalf("production --instance: want dev-only error, got %v", err)
	}

	// Development uses the checkout's instance.
	t.Setenv("SCHEMAF_DEV", "1")
	a = &App{project: "myapp", projectRoot: root}
	if err := a.resolveInstance(cmd); err != nil || constants.Instance() != 349 {
		t.Fatalf("dev: instance %d, %v", constants.Instance(), err)
	}
	if got := a.dsn(); !strings.Contains(got, ":"+strconv.Itoa(constants.ConfigPort()+3493)+"/myapp?") {
		t.Errorf("DSN must reach instance 349's Postgres: %s", got)
	}
}

// The instance is resolved from the command line before cobra runs anything,
// so an app subcommand with its own PersistentPreRunE still gets it, without
// changing cobra's hook order for the whole process.
func TestResolveInstanceBeforeExecute(t *testing.T) {
	old := constants.Instance()
	t.Cleanup(func() { _ = constants.SetInstance(old) })
	if !constants.IsProjectNameSet() {
		constants.SetProjectName("myapp")
	}
	t.Setenv("SCHEMAF_DEV", "1")

	a := &App{project: "myapp", projectRoot: t.TempDir()}
	root := &cobra.Command{Use: "app"}
	a.addInstanceFlag(root)
	ranOwnHook := false
	root.AddCommand(&cobra.Command{
		Use:               "export",
		PersistentPreRunE: func(*cobra.Command, []string) error { ranOwnHook = true; return nil },
		RunE:              func(*cobra.Command, []string) error { return nil },
	})

	if err := a.resolveInstanceFor(root, []string{"export", "--instance", "7"}); err != nil {
		t.Fatal(err)
	}
	if constants.Instance() != 7 {
		t.Fatalf("app subcommand with its own hook: instance %d, want 7", constants.Instance())
	}
	root.SetArgs([]string{"export", "--instance", "7"})
	if err := root.Execute(); err != nil || !ranOwnHook {
		t.Fatalf("the subcommand's own hook must still run: %v, ran=%v", err, ranOwnHook)
	}
	if cobra.EnableTraverseRunHooks {
		t.Error("cobra.EnableTraverseRunHooks must stay off: app subcommands keep cobra's default hook order")
	}

	// A command line cobra can't parse resolves nothing; Execute reports it.
	a.instanceFlag = ""
	if err := a.resolveInstanceFor(root, []string{"export", "--no-such-flag"}); err != nil {
		t.Fatalf("unparseable command line: want nil (Execute reports it), got %v", err)
	}
}
