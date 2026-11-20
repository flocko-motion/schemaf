package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/constants"
)

func stickyFile(t *testing.T, root string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ".schemaf", "instance"))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func TestInstanceStickyFile(t *testing.T) {
	root := t.TempDir()

	// No file, no flag: the default instance.
	if n, err := ResolveInstance(root, "", false); err != nil || n != 0 {
		t.Fatalf("fresh checkout: %d, %v", n, err)
	}

	// dev --instance 349 writes the file, with its own .gitignore.
	if n, err := ResolveInstance(root, "349", true); err != nil || n != 349 {
		t.Fatalf("dev --instance 349: %d, %v", n, err)
	}
	if err := PersistInstance(root, "349"); err != nil {
		t.Fatal(err)
	}
	if got, ok := stickyFile(t, root); !ok || strings.TrimSpace(got) != "349" {
		t.Fatalf("instance file: %q, %v", got, ok)
	}
	if gi, err := os.ReadFile(filepath.Join(root, ".schemaf", ".gitignore")); err != nil || strings.TrimSpace(string(gi)) != "*" {
		t.Fatalf(".schemaf/.gitignore: %q, %v", gi, err)
	}

	// Later commands use it without the flag, and with the same flag.
	for _, flag := range []string{"", "349"} {
		for _, dev := range []bool{false, true} {
			if n, err := ResolveInstance(root, flag, dev); err != nil || n != 349 {
				t.Errorf("flag %q dev=%v: %d, %v", flag, dev, n, err)
			}
		}
	}

	// A different explicit instance fails, in dev and elsewhere.
	for _, dev := range []bool{false, true} {
		_, err := ResolveInstance(root, "350", dev)
		if err == nil || !strings.Contains(err.Error(), "349") || !strings.Contains(err.Error(), "350") {
			t.Errorf("--instance 350 dev=%v: want conflict naming both, got %v", dev, err)
		}
	}

	// --instance default conflicts outside dev, but dev switches back.
	if _, err := ResolveInstance(root, "default", false); err == nil {
		t.Error("db --instance default in an instance-349 checkout: want conflict")
	}
	if n, err := ResolveInstance(root, "default", true); err != nil || n != 0 {
		t.Fatalf("dev --instance default: %d, %v", n, err)
	}
	if err := PersistInstance(root, "default"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stickyFile(t, root); ok {
		t.Fatal("dev --instance default must remove the instance file")
	}
	if n, err := ResolveInstance(root, "", false); err != nil || n != 0 {
		t.Fatalf("after switching back: %d, %v", n, err)
	}

	// Without the file, other commands take --instance for one call only.
	if n, err := ResolveInstance(root, "7", false); err != nil || n != 7 {
		t.Fatalf("db --instance 7: %d, %v", n, err)
	}
	if _, ok := stickyFile(t, root); ok {
		t.Fatal("resolving must never write the instance file")
	}

	// Without a flag, nothing is persisted (and nothing removed).
	if err := PersistInstance(root, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := stickyFile(t, root); ok {
		t.Fatal("PersistInstance without a flag wrote the file")
	}
}

func TestInstanceInvalidInput(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveInstance(root, "abc", true); err == nil {
		t.Error("--instance abc: want error")
	}
	if err := os.MkdirAll(filepath.Join(root, ".schemaf"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".schemaf", "instance"), []byte("nope\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInstance(root, "", false)
	if err == nil || !strings.Contains(err.Error(), ".schemaf/instance") {
		t.Errorf("corrupt instance file: want error naming the file, got %v", err)
	}
}

// withProject sets the project name and a fake HOME for one test.
func withProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := ""
	if constants.IsProjectNameSet() {
		old = constants.ProjectName()
	}
	oldInst := constants.Instance()
	t.Cleanup(func() {
		constants.SetProjectName(old)
		_ = constants.SetInstance(oldInst)
	})
	constants.SetProjectName("myapp")
	return home
}

func TestInstanceRecords(t *testing.T) {
	home := withProject(t)

	if recs, err := InstanceRecords(); err != nil || len(recs) != 0 {
		t.Fatalf("no instances yet: %v, %v", recs, err)
	}
	for _, r := range []InstanceRecord{
		{Instance: 349, BackendPort: 10590, VitePort: 10592, PostgresPort: 10593, Checkout: "/src/myapp-349"},
		{Instance: 7, BackendPort: 8070, VitePort: 8072, PostgresPort: 8073, Checkout: "/src/myapp-7"},
	} {
		if err := WriteInstanceRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".myapp", "dev", "instances", "349", "instance.toml")); err != nil {
		t.Fatalf("record not at ~/.myapp/dev/instances/349/instance.toml: %v", err)
	}

	recs, err := InstanceRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Instance != 7 || recs[1].Instance != 349 {
		t.Fatalf("want instances 7 and 349 in order, got %+v", recs)
	}
	if r := recs[1]; r.BackendPort != 10590 || r.VitePort != 10592 || r.PostgresPort != 10593 || r.Checkout != "/src/myapp-349" {
		t.Fatalf("record 349 round trip: %+v", r)
	}
	if r, ok, err := ReadInstanceRecord(7); err != nil || !ok || r.Checkout != "/src/myapp-7" {
		t.Fatalf("ReadInstanceRecord(7): %+v %v %v", r, ok, err)
	}
	if _, ok, err := ReadInstanceRecord(8); err != nil || ok {
		t.Fatalf("ReadInstanceRecord(8): %v %v", ok, err)
	}
}

func TestInstanceDataDir(t *testing.T) {
	home := withProject(t)
	t.Setenv("SCHEMAF_DEV", "1")

	if got, want := VarDir(), filepath.Join(home, ".myapp", "dev", "var"); got != want {
		t.Errorf("default instance: %s, want %s", got, want)
	}
	if err := constants.SetInstance(349); err != nil {
		t.Fatal(err)
	}
	if got, want := VarDir(), filepath.Join(home, ".myapp", "dev", "instances", "349", "var"); got != want {
		t.Errorf("instance 349: %s, want %s", got, want)
	}
	// Secrets stay shared.
	if got, want := ConfigDir(), filepath.Join(home, ".myapp", "dev", "etc"); got != want {
		t.Errorf("instance 349 config: %s, want %s", got, want)
	}
}

// An instance belongs to one checkout. A recorded checkout that no longer
// claims the instance (gone, or its .schemaf/instance names something else)
// is taken over; one that still claims it is a conflict.
func TestClaimInstance(t *testing.T) {
	withProject(t)
	here, other := t.TempDir(), t.TempDir()
	record := func(checkout string) {
		t.Helper()
		if err := WriteInstanceRecord(InstanceRecord{Instance: 5, Checkout: checkout}); err != nil {
			t.Fatal(err)
		}
	}

	// No record yet, or recorded for this checkout: ours.
	if took, err := ClaimInstance(5, here); err != nil || took != "" {
		t.Fatalf("no record: %q, %v", took, err)
	}
	record(here)
	if took, err := ClaimInstance(5, here); err != nil || took != "" {
		t.Fatalf("own record: %q, %v", took, err)
	}

	// The other checkout still claims 5: refused, naming it and the way out.
	if err := PersistInstance(other, "5"); err != nil {
		t.Fatal(err)
	}
	record(other)
	_, err := ClaimInstance(5, here)
	if err == nil || !strings.Contains(err.Error(), other) || !strings.Contains(err.Error(), "instances rm 5") {
		t.Fatalf("claimed elsewhere: want conflict naming %s, got %v", other, err)
	}

	// It switched to another instance: taken over.
	if err := PersistInstance(other, "default"); err != nil {
		t.Fatal(err)
	}
	if took, err := ClaimInstance(5, here); err != nil || took != other {
		t.Fatalf("other checkout without the file: took %q, %v", took, err)
	}

	// It is gone (e.g. git worktree move): taken over.
	record(filepath.Join(other, "moved-away"))
	if took, err := ClaimInstance(5, here); err != nil || took != filepath.Join(other, "moved-away") {
		t.Fatalf("checkout gone: took %q, %v", took, err)
	}
}
