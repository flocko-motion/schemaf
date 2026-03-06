package codegen

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

//go:embed ts_test.gen.go.tmpl
var tsTestGenTemplate string

//go:embed runner.gen.ts.tmpl
var runnerGenTemplate string

const testsDir = "go/tests"

var reExportedTestFunc = regexp.MustCompile(`^export\s+async\s+function\s+(test\w+)`)
var reSkipComment = regexp.MustCompile(`//\s*skip:\s*(.+)`)

func newTestsCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "tests",
		Short: "Generate Go wrappers for TypeScript tests",
		Long:  `Scans go/tests/*.test.ts and generates ts_test.gen.go and runner.gen.ts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTestsGen()
		},
	}
}

type tsTestFunc struct {
	FuncName string // e.g. "testHealth"
	GoName   string // e.g. "Health"
	Skip     string // non-empty if the test should be skipped
}

type tsTestFile struct {
	Name  string // e.g. "api.test.ts"
	Alias string // e.g. "m0"
	Tests []tsTestFunc
}

func runTestsGen() error {
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		cli.Info("no %s directory — skipping tests codegen", testsDir)
		return nil
	}

	entries, err := os.ReadDir(testsDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", testsDir, err)
	}

	module, err := readModuleName()
	if err != nil {
		return err
	}

	var files []tsTestFile
	for i, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".test.ts") {
			continue
		}
		tests, err := parseTSTestFuncs(filepath.Join(testsDir, e.Name()))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", e.Name(), err)
		}
		if len(tests) == 0 {
			continue
		}
		files = append(files, tsTestFile{
			Name:  e.Name(),
			Alias: fmt.Sprintf("m%d", i),
			Tests: tests,
		})
	}

	if len(files) == 0 {
		cli.Info("no *.test.ts files found in %s — skipping", testsDir)
		return nil
	}

	// Flatten all tests for the Go template
	var allTests []tsTestFunc
	for _, f := range files {
		allTests = append(allTests, f.Tests...)
	}

	if err := renderTemplate(tsTestGenTemplate, filepath.Join(testsDir, "ts.gen_test.go"), map[string]any{
		"Module": module,
		"Tests":  allTests,
	}); err != nil {
		return err
	}
	cli.Success("Generated %s (%d test(s))", filepath.Join(testsDir, "ts.gen_test.go"), len(allTests))

	if err := renderTemplate(runnerGenTemplate, filepath.Join(testsDir, "runner.gen.ts"), map[string]any{
		"Files": files,
	}); err != nil {
		return err
	}
	cli.Success("Generated %s", filepath.Join(testsDir, "runner.gen.ts"))

	return nil
}

// parseTSTestFuncs scans a .test.ts file for exported async test functions.
func parseTSTestFuncs(path string) ([]tsTestFunc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tests []tsTestFunc
	var pendingSkip string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Check for skip comment on the preceding line
		if m := reSkipComment.FindStringSubmatch(line); m != nil {
			pendingSkip = strings.TrimSpace(m[1])
			continue
		}
		if m := reExportedTestFunc.FindStringSubmatch(line); m != nil {
			funcName := m[1]
			tests = append(tests, tsTestFunc{
				FuncName: funcName,
				GoName:   tsToGoTestName(funcName),
				Skip:     pendingSkip,
			})
			pendingSkip = ""
		} else {
			pendingSkip = ""
		}
	}
	return tests, scanner.Err()
}

// tsToGoTestName converts "testClockTime" → "ClockTime".
func tsToGoTestName(name string) string {
	name = strings.TrimPrefix(name, "test")
	if len(name) == 0 {
		return name
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// readModuleName reads the module name from go/go.mod.
func readModuleName() (string, error) {
	f, err := os.Open("go/go.mod")
	if err != nil {
		return "", fmt.Errorf("opening go/go.mod: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}
	return "", fmt.Errorf("go/go.mod: no module line found")
}

