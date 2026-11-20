package schemaf

import (
	"testing"

	"github.com/flocko-motion/schemaf/files"
)

// instances shows the ports dev would use now, calculated from schemaf.toml's
// port; a record from before a port change is marked, never shown as current.
func TestInstanceRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  files.InstanceRecord
		base int
		want string
	}{
		{"default instance", files.InstanceRecord{}, 7100,
			"default\tyes\t7100\t7102\t7103\t-"},
		{"recorded ports match", files.InstanceRecord{Instance: 349, BackendPort: 10590, VitePort: 10592, PostgresPort: 10593, Checkout: "/src/a"}, 7100,
			"349\tyes\t10590\t10592\t10593\t/src/a"},
		{"port changed since the record", files.InstanceRecord{Instance: 349, BackendPort: 10590, VitePort: 10592, PostgresPort: 10593, Checkout: "/src/a"}, 7200,
			"349\tyes\t10690\t10692\t10693\t/src/a  (recorded ports 10590/10592/10593; the next dev updates the record)"},
		{"port change pushes it out of range", files.InstanceRecord{Instance: 5000, BackendPort: 58000, VitePort: 58002, PostgresPort: 58003, Checkout: "/src/b"}, 20000,
			"5000\tyes\t-\t-\t-\t/src/b  (base port 70000 is above 65525 with port = 20000; remove it: instances rm 5000)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := instanceRow(tc.rec, tc.base, "yes"); got != tc.want {
				t.Errorf("\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}
