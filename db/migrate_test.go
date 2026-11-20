// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"embed"
	"fmt"
	"strings"
	"testing"
)

// TestFrameworkMigrationsRunFirst verifies that the "schemaf" prefix is always
// ordered first in runMigrations, regardless of registration order.
func TestFrameworkMigrationsRunFirst(t *testing.T) {
	// Simulate a scenario where project migrations were registered before framework
	// migrations (e.g. via a rogue init() ordering). runMigrations must still put
	// "schemaf" first.
	sets := []MigrationSet{
		{Prefix: "myproject"},
		{Prefix: "schemaf"},
		{Prefix: "another"},
	}

	ordered := orderMigrationSets(sets)

	if ordered[0].Prefix != "schemaf" {
		t.Fatalf("expected first set to be \"schemaf\", got %q", ordered[0].Prefix)
	}
}

func TestFrameworkMigrationsFirstWhenAlreadyFirst(t *testing.T) {
	sets := []MigrationSet{
		{Prefix: "schemaf"},
		{Prefix: "myproject"},
	}

	ordered := orderMigrationSets(sets)

	if ordered[0].Prefix != "schemaf" {
		t.Fatalf("expected first set to be \"schemaf\", got %q", ordered[0].Prefix)
	}
	if ordered[1].Prefix != "myproject" {
		t.Fatalf("expected second set to be \"myproject\", got %q", ordered[1].Prefix)
	}
}

func TestNoSchemafPrefix(t *testing.T) {
	sets := []MigrationSet{
		{Prefix: "alpha"},
		{Prefix: "beta"},
	}

	ordered := orderMigrationSets(sets)

	// Order should be preserved when no "schemaf" prefix is present.
	if ordered[0].Prefix != "alpha" || ordered[1].Prefix != "beta" {
		t.Fatalf("unexpected order: %v", ordered)
	}
}

// Registering a set twice (AddDb called again, e.g. by every schemaftest
// instance) keeps one set per prefix: the later registration replaces the
// earlier one in place.
func TestRegisterMigrationsDeduplicatesByPrefix(t *testing.T) {
	saved := registeredSets
	t.Cleanup(func() { registeredSets = saved })
	registeredSets = nil

	RegisterMigrations(MigrationSet{Prefix: "schemaf"})
	RegisterMigrations(MigrationSet{Prefix: "myapp"})
	RegisterMigrations(MigrationSet{Prefix: "myapp"})
	RegisterMigrations(MigrationSet{Prefix: "schemaf"})

	var got []string
	for _, s := range registeredSets {
		got = append(got, s.Prefix)
	}
	if len(got) != 2 || got[0] != "schemaf" || got[1] != "myapp" {
		t.Fatalf("registered sets = %v, want [schemaf myapp]", got)
	}
}

//go:embed testdata/runset_widgets/*.sql
var clashA embed.FS

//go:embed testdata/runset_sentinel/*.sql
var clashB embed.FS

// The same prefix with different files is a wiring bug: it panics, naming
// the prefix, instead of silently replacing the first set.
func TestRegisterMigrationsPanicsOnDifferentFiles(t *testing.T) {
	saved := registeredSets
	t.Cleanup(func() { registeredSets = saved })
	registeredSets = nil

	RegisterMigrations(MigrationSet{Prefix: "myapp", Files: clashA})
	RegisterMigrations(MigrationSet{Prefix: "myapp", Files: clashA}) // same files: fine
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), `"myapp"`) {
			t.Fatalf("recovered %v, want a panic naming the prefix", r)
		}
	}()
	RegisterMigrations(MigrationSet{Prefix: "myapp", Files: clashB})
}
