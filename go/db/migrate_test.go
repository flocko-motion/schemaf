package db

import (
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
