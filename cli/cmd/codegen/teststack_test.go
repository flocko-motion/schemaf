package codegen

import (
	"os"
	"strings"
	"testing"
)

// testBranch returns the `test)` branch of the generated schemaf.sh.
func testBranch(t *testing.T) string {
	t.Helper()
	s := readFile(t, "schemaf.sh")
	i := strings.Index(s, "\n  test)\n")
	if i < 0 {
		t.Fatal("schemaf.sh has no test) branch")
	}
	rest := s[i:]
	return rest[:strings.Index(rest, "\n    ;;")]
}

// The test stack holds only the project's services: no Postgres, and
// schemaf.sh test exports no DATABASE_URL (EXTEND.md › Testing).
func TestTestStackHasNoPostgres(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	stack := readFile(t, "compose.test.gen.yml")
	if strings.Contains(stack, "postgres") {
		t.Errorf("compose.test.gen.yml still has postgres:\n%s", stack)
	}
	for _, svc := range []string{"redis:", "tools:"} {
		if !strings.Contains(stack, svc) && !strings.Contains(stack, "compose/") {
			t.Errorf("compose.test.gen.yml lacks the project service %s:\n%s", svc, stack)
		}
	}
	branch := testBranch(t)
	if strings.Contains(branch, "DATABASE_URL") || strings.Contains(branch, "postgres") {
		t.Errorf("schemaf.sh test still sets up Postgres:\n%s", branch)
	}
	if !strings.Contains(branch, "compose -f compose.test.gen.yml up") {
		t.Errorf("schemaf.sh test doesn't start the project services:\n%s", branch)
	}
}

// Without project services, schemaf.sh test starts no compose stack.
func TestTestWithoutServicesStartsNoStack(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := os.RemoveAll("compose"); err != nil {
		t.Fatal(err)
	}
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	if branch := testBranch(t); strings.Contains(branch, "compose.test.gen.yml") {
		t.Errorf("schemaf.sh test touches the compose stack without services:\n%s", branch)
	}
}

// A test-stack service that depends on postgres would start without one:
// codegen stops, naming it.
func TestTestStackServiceDependingOnPostgresFails(t *testing.T) {
	for name, file := range map[string]string{
		"list form": "services:\n  worker:\n    image: busybox\n    depends_on:\n      - postgres\n",
		"map form":  "services:\n  worker:\n    image: busybox\n    depends_on:\n      postgres:\n        condition: service_healthy\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeMinimalProject(t)
			writeFiles(t, map[string]string{"compose/worker.dev.yml": file})
			err := runComposeGen()
			if err == nil || !strings.Contains(err.Error(), "worker") || !strings.Contains(err.Error(), "compose/worker.dev.yml") ||
				!strings.Contains(err.Error(), "postgres") {
				t.Fatalf("runComposeGen = %v, want an error naming worker in compose/worker.dev.yml", err)
			}
		})
	}
}
