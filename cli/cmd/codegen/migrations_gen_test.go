package codegen

import (
	"strings"
	"testing"
)

// No migrations: the generated file compiles (no //go:embed pattern that
// matches nothing) and never needs a placeholder migration.
func TestMigrationsGenNoMigrationsCompiles(t *testing.T) {
	src, err := renderMigrationsGen(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "go:embed") {
		t.Errorf("generated file embeds with no migrations:\n%s", src)
	}
	compileGenerated(t, map[string]string{
		"db/migrations.gen.go": string(src),
		"db/migrations/":       "",
	})
}

func TestMigrationsGenWithMigrationsCompiles(t *testing.T) {
	src, err := renderMigrationsGen(true)
	if err != nil {
		t.Fatal(err)
	}
	compileGenerated(t, map[string]string{
		"db/migrations.gen.go":        string(src),
		"db/migrations/0001_init.sql": "CREATE TABLE t (id int);\n",
	})
}
