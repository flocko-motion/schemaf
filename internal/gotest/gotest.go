// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// Package gotest is test support for framework tests that run the go tool
// themselves. Any test that runs it with GOPROXY=off (e.g. go build in a
// temporary module that replaces schemaf with this checkout) must call
// RequireModuleCache first: offline, the go tool finds only what is in the
// module cache, and building schemaf doesn't put its whole module graph there
// (modules only other platforms compile, like cobra's Windows-only mousetrap,
// are never downloaded), so on a fresh cache such a test would fail.
package gotest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	once        sync.Once
	downloadErr error
)

// RequireModuleCache makes sure schemaf's full module graph is in the module
// cache: it runs `go mod download` in the schemaf checkout once per test
// binary, with the caller's GOPROXY (GOFLAGS cleared, GOWORK=off). If that
// fails, the test fails with go's output. Call it before going offline.
func RequireModuleCache(t testing.TB) {
	t.Helper()
	once.Do(func() {
		root, err := checkout()
		if err != nil {
			downloadErr = err
			return
		}
		cmd := exec.Command("go", "mod", "download")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			downloadErr = fmt.Errorf("`go mod download` in %s failed: %v\n%s", root, err, out)
		}
	})
	if downloadErr != nil {
		t.Fatalf("this test runs the go tool offline (GOPROXY=off) and needs schemaf's full module graph in the module cache: %v", downloadErr)
	}
}

// checkout returns the root of the schemaf checkout, two levels above this file.
func checkout() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate the schemaf checkout")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.HasPrefix(string(gomod), "module github.com/flocko-motion/schemaf\n") {
		return "", fmt.Errorf("%s is not the schemaf checkout", root)
	}
	return root, nil
}
