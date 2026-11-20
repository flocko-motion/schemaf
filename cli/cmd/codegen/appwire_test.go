package codegen

import (
	"strings"
	"testing"
)

// Codegen stops, with the migration message, until go/app/app.go defines
// func Wire(app *schemaf.App) (EXTEND.md › App Wiring).
func TestCheckAppWire(t *testing.T) {
	const msg = "move your wiring from go/main.go into func Wire in go/app/app.go"
	for _, tc := range []struct {
		name  string
		files map[string]string
		ok    bool
	}{
		{"no go/app/app.go", map[string]string{"go/main.go": "package main\n"}, false},
		{"no Wire", map[string]string{"go/app/app.go": "package app\n\nfunc Setup() {}\n"}, false},
		{"Wire without a parameter", map[string]string{"go/app/app.go": "package app\n\nfunc Wire() {}\n"}, false},
		{"Wire as a method", map[string]string{"go/app/app.go": "package app\n\ntype T struct{}\n\nfunc (T) Wire(app *schemaf.App) {}\n"}, false},
		{"Wire", map[string]string{"go/app/app.go": "package app\n\nimport \"github.com/flocko-motion/schemaf/schemaf\"\n\nfunc Wire(app *schemaf.App) {}\n"}, true},
		{"Wire, aliased import", map[string]string{"go/app/app.go": "package app\n\nimport sf \"github.com/flocko-motion/schemaf/schemaf\"\n\nfunc Wire(a *sf.App) {}\n"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFiles(t, tc.files)
			err := checkAppWire()
			if tc.ok {
				if err != nil {
					t.Fatalf("checkAppWire: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), msg) {
				t.Fatalf("checkAppWire = %v, want the migration message", err)
			}
		})
	}
}
