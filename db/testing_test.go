// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import "testing"

func TestIsProjectTable(t *testing.T) {
	cases := []struct {
		name     string
		table    string
		expected bool
	}{
		// Framework tables — must NOT be reset
		{"schemaf_migrations", "schemaf_migrations", false},
		{"schemaf_sessions", "schemaf_sessions", false},
		{"_schemaf_config", "_schemaf_config", false},
		{"_schemaf_keys", "_schemaf_keys", false},

		// Project tables — must be reset
		{"users", "users", true},
		{"todos", "todos", true},
		{"posts", "posts", true},
		{"schemaf", "schemaf", true}, // exact match, not a prefix match
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isProjectTable(tc.table)
			if got != tc.expected {
				t.Errorf("isProjectTable(%q) = %v, want %v", tc.table, got, tc.expected)
			}
		})
	}
}
