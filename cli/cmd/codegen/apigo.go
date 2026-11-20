// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"os"
	"os/exec"

	cli "github.com/flocko-motion/schemaf/cli"
)

// The oapi-codegen generator and runtime a project gets, pinned: an unversioned
// `go get` takes whatever is newest, so init would not be reproducible, and a
// release that needs a newer Go than schemaf's minimum (the go directive in
// schemaf's go.mod) would break every new project on that Go. Each pin is the
// newest release whose own go directive is <= schemaf's; TestOapiPinsFitSchemafGo
// checks the whole resulting module graph. Raise them together with schemaf's
// go directive.
const (
	oapiCodegenPkg     = "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
	oapiCodegenModule  = "github.com/oapi-codegen/oapi-codegen/v2"
	oapiCodegenVersion = "v2.8.2" // go 1.25.0
	oapiRuntimePkg     = "github.com/oapi-codegen/runtime"
	oapiRuntimeModule  = oapiRuntimePkg
	oapiRuntimeVersion = "v1.8.0" // go 1.24.0 (v1.9.0 needs go 1.26.0)
)

// oapiGetArgs are the `go get` arguments for the pinned generator and runtime.
func oapiGetArgs() (codegen, runtime []string) {
	return []string{"get", oapiCodegenPkg + "@" + oapiCodegenVersion},
		[]string{"get", oapiRuntimePkg + "@" + oapiRuntimeVersion}
}

func runAPIGo() error {
	if _, err := os.Stat("gen/openapi.json"); os.IsNotExist(err) {
		cli.Warning("gen/openapi.json not found — skipping Go client generation")
		return nil
	}

	if err := os.MkdirAll("go/apiclient", 0755); err != nil {
		return fmt.Errorf("creating go/apiclient/: %w", err)
	}

	getCodegen, getRuntime := oapiGetArgs()

	// Ensure oapi-codegen is available as a module dependency, at its pin;
	// `go run` below then runs that version.
	get := exec.Command("go", getCodegen...)
	get.Dir = "go"
	get.Stdout = os.Stdout
	get.Stderr = os.Stderr
	if err := get.Run(); err != nil {
		return fmt.Errorf("go get oapi-codegen: %w", err)
	}

	cmd := exec.Command("go", "run", oapiCodegenPkg,
		"--package", "apiclient",
		"--generate", "types,client",
		"-o", "apiclient/client.gen.go",
		"../gen/openapi.json",
	)
	cmd.Dir = "go"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oapi-codegen: %w", err)
	}

	// The generated client imports the oapi-codegen runtime library for path
	// parameters (e.g. /api/auth/{provider}/login, always in the spec).
	// Make it a module dependency so the project compiles.
	rt := exec.Command("go", getRuntime...)
	rt.Dir = "go"
	rt.Stdout = os.Stdout
	rt.Stderr = os.Stderr
	if err := rt.Run(); err != nil {
		return fmt.Errorf("go get oapi-codegen runtime: %w", err)
	}
	cli.Success("Generated go/apiclient/client.gen.go")
	return nil
}
