// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

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

	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed ts_test.gen.go.tmpl
var tsTestGenTemplate string

//go:embed runner.gen.ts.tmpl
var runnerGenTemplate string

const testsDir = "go/tests"

var reExportedTestFunc = regexp.MustCompile(`^export\s+async\s+function\s+(test\w+)`)
var reSkipComment = regexp.MustCompile(`//\s*skip:\s*(.+)`)
var reManualComment = regexp.MustCompile(`//\s*manual:`)

type tsTestFunc struct {
	FuncName string // e.g. "testHealth"
	GoName   string // e.g. "Health"
	Skip     string // non-empty if the test should be skipped
	Manual   bool   // true if a hand-written Go wrapper exists — skip codegen
}

type tsTestFile struct {
	Name     string // e.g. "api.test.ts"
	Alias    string // e.g. "m0"
	GoName   string // e.g. "Api": the Go test is TestTSApi
	Runnable bool   // at least one test isn't skipped, so the file starts an instance
	Tests    []tsTestFunc
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
	goNames := map[string]string{} // Go test name → .test.ts file
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
		goName := tsFileToGoName(e.Name())
		if other, ok := goNames[goName]; ok {
			return fmt.Errorf("%s and %s both become the Go test TestTS%s: rename one", other, e.Name(), goName)
		}
		goNames[goName] = e.Name()
		runnable := false
		for _, tf := range tests {
			if tf.Skip == "" {
				runnable = true
			}
		}
		files = append(files, tsTestFile{
			Name:     e.Name(),
			Alias:    fmt.Sprintf("m%d", i),
			GoName:   goName,
			Runnable: runnable,
			Tests:    tests,
		})
	}

	// No TS tests: the wrappers are still generated, with no tests, so they
	// always match go/tests (EXTEND.md › Empty inputs). Tests are optional,
	// so this is not an empty input and there is no warning.

	n, anyRunnable := 0, false
	for _, f := range files {
		n += len(f.Tests)
		anyRunnable = anyRunnable || f.Runnable
	}

	if err := renderTemplate(tsTestGenTemplate, filepath.Join(testsDir, "ts.gen_test.go"), map[string]any{
		"Module":   module,
		"Files":    files,
		"Runnable": anyRunnable, // the harness imports are used only then
	}); err != nil {
		return err
	}
	cli.Success("Generated %s (%d test(s) in %d file(s))", filepath.Join(testsDir, "ts.gen_test.go"), n, len(files))

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
	var pendingManual bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if m := reSkipComment.FindStringSubmatch(line); m != nil {
			pendingSkip = strings.TrimSpace(m[1])
			continue
		}
		if reManualComment.MatchString(line) {
			pendingManual = true
			continue
		}
		if m := reExportedTestFunc.FindStringSubmatch(line); m != nil {
			funcName := m[1]
			if !pendingManual {
				tests = append(tests, tsTestFunc{
					FuncName: funcName,
					GoName:   tsToGoTestName(funcName),
					Skip:     pendingSkip,
				})
			}
			pendingSkip = ""
			pendingManual = false
		} else {
			pendingSkip = ""
			pendingManual = false
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

// tsFileToGoName converts "user-me.test.ts" → "UserMe": the file name
// without ".test.ts", split at every non-alphanumeric rune, each part
// capitalized.
func tsFileToGoName(name string) string {
	base := strings.TrimSuffix(name, ".test.ts")
	var b strings.Builder
	for _, part := range strings.FieldsFunc(base, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		r := []rune(part)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
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

