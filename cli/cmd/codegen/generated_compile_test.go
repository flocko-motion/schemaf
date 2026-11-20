package codegen

import (
	"strings"
	"testing"
)

// frontend.gen.go embeds go/frontend_dist/. Before the frontend build fills
// it, the directory holds only .gitkeep, the one placeholder codegen keeps
// (EXTEND.md › Empty inputs).
func TestFrontendGenOnlyGitkeepCompiles(t *testing.T) {
	compileGenerated(t, map[string]string{
		"frontend.gen.go":        frontendGenTemplate,
		"frontend_dist/.gitkeep": "",
		"main.go":                mainStub,
	})
}

func TestConstantsGenCompiles(t *testing.T) {
	src := renderForTest(t, constantsGenTemplate, map[string]any{"Name": "myapp", "Port": 8000})
	compileGenerated(t, map[string]string{
		"constants.gen.go": src,
		"main.go":          mainStub,
	})
}

// With no TS tests, ts.gen_test.go holds only its helpers. go vet type-checks
// it (go build skips _test files).
func TestTSTestGenNoTestsCompiles(t *testing.T) {
	endpoints, err := renderEndpointsGen(nil, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	src := renderForTest(t, tsTestGenTemplate, map[string]any{"Module": compileModule, "Tests": []tsTestFunc(nil)})
	if strings.Contains(src, "func Test") {
		t.Errorf("generated file has tests with no TS tests:\n%s", src)
	}
	compileGenerated(t, map[string]string{
		"api/endpoints.gen.go": string(endpoints),
		"tests/ts.gen_test.go": src,
	})
}

// renderForTest renders a template the way renderTemplate does, into a string.
func renderForTest(t *testing.T, tmpl string, data map[string]any) string {
	t.Helper()
	out := t.TempDir() + "/out"
	if err := renderTemplate(tmpl, out, data); err != nil {
		t.Fatal(err)
	}
	return readFile(t, out)
}

// go/tests/ with no TS tests: the wrappers are generated with no tests, so a
// stale list never survives the last test's removal.
func TestTestsGenNoTestsGeneratesEmptyWrappers(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFiles(t, map[string]string{
		"go/go.mod":               "module myapp\n",
		"go/tests/ts.gen_test.go": "stale",
		"go/tests/helpers.ts":     "export const x = 1\n",
	})
	if err := runTestsGen(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, "go/tests/ts.gen_test.go"); strings.Contains(got, "func Test") || !strings.Contains(got, "func runTSTest") {
		t.Errorf("ts.gen_test.go with no TS tests:\n%s", got)
	}
	if got := readFile(t, "go/tests/runner.gen.ts"); strings.Contains(got, "import ") {
		t.Errorf("runner.gen.ts with no TS tests imports a test file:\n%s", got)
	}
}
