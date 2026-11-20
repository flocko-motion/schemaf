package codegen

import (
	"strings"
	"testing"
)

// appStub is a project's go/app with a Wire, as the generated TS wrappers
// import it.
const appStub = "package app\n\nimport \"github.com/flocko-motion/schemaf/schemaf\"\n\nfunc Wire(app *schemaf.App) {}\n"

// One Go test per .test.ts file starts one instance; the file's test*
// functions run as its subtests and get the URL and a token. A file whose
// tests are all skipped starts no instance (EXTEND.md › TypeScript tests).
func TestTSWrappersOneInstancePerFile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"go/go.mod": "module " + compileModule + "\n",
		"go/tests/api.test.ts": "export async function testCreateUser(baseUrl: string, token: string) {}\n" +
			"export async function testListUsers(baseUrl: string) {}\n",
		"go/tests/user-me.test.ts": "export async function testMe(baseUrl: string, token: string) {}\n",
		"go/tests/clock.test.ts":   "// skip: needs the clock service\nexport async function testClock(baseUrl: string) {}\n",
	})
	if err := runTestsGen(); err != nil {
		t.Fatal(err)
	}
	src := readFile(t, "go/tests/ts.gen_test.go")
	for _, want := range []string{
		"func TestTSApi(t *testing.T)",
		"func TestTSUserMe(t *testing.T)",
		"func TestTSClock(t *testing.T)",
		`t.Run("CreateUser"`,
		`t.Run("ListUsers"`,
		`t.Skip("needs the clock service")`,
		"schemaftest.Start(t, app.Wire)",
		`inst.Token("test")`,
		"/app\"",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("ts.gen_test.go lacks %q:\n%s", want, src)
		}
	}
	if n := strings.Count(src, "schemaftest.Start(t, app.Wire)"); n != 2 {
		t.Errorf("schemaftest.Start appears %d times, want 2 (api, user-me; clock is all skipped):\n%s", n, src)
	}
	if strings.Contains(src, "httptest") || strings.Contains(src, "Reset()") {
		t.Errorf("ts.gen_test.go still uses the old httptest server:\n%s", src)
	}
	runner := readFile(t, "go/tests/runner.gen.ts")
	if !strings.Contains(runner, "token") {
		t.Errorf("runner.gen.ts doesn't pass the token:\n%s", runner)
	}

	compileGenerated(t, map[string]string{
		"app/app.go":           appStub,
		"tests/ts.gen_test.go": src,
	})
}

// Two files whose names map to the same Go test name are an error.
func TestTSWrappersNameCollision(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"go/go.mod":            "module myapp\n",
		"go/tests/a-b.test.ts": "export async function testX(baseUrl: string) {}\n",
		"go/tests/a_b.test.ts": "export async function testY(baseUrl: string) {}\n",
	})
	err := runTestsGen()
	if err == nil || !strings.Contains(err.Error(), "a-b.test.ts") || !strings.Contains(err.Error(), "a_b.test.ts") {
		t.Fatalf("runTestsGen = %v, want a collision error naming both files", err)
	}
}

// Every test skipped: no file starts an instance, so the harness imports
// must go too, or the generated file doesn't compile.
func TestTSWrappersAllSkippedCompiles(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"go/go.mod":              "module " + compileModule + "\n",
		"go/tests/clock.test.ts": "// skip: needs the clock service\nexport async function testClock(baseUrl: string) {}\n",
	})
	if err := runTestsGen(); err != nil {
		t.Fatal(err)
	}
	compileGenerated(t, map[string]string{
		"app/app.go":           appStub,
		"tests/ts.gen_test.go": readFile(t, "go/tests/ts.gen_test.go"),
	})
}
