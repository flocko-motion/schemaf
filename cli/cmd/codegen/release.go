// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed release.gen.yml.tmpl
var releaseWorkflowTemplate string

const releaseWorkflowPath = ".github/workflows/release.gen.yml"

// runReleaseGen writes the release workflow: a pushed tag vX.Y.Z builds,
// attests and publishes the artifact and releases.json
// (INSTALL.md › Releases from CI).
func runReleaseGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}
	// GitHub's own ${{ }} expressions fill the file, so the template uses {% %}.
	tmpl, err := template.New("release").Delims("{%", "%}").Parse(releaseWorkflowTemplate)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", releaseWorkflowPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"Name": name}); err != nil {
		return fmt.Errorf("executing template %s: %w", releaseWorkflowPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(releaseWorkflowPath), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(releaseWorkflowPath), err)
	}
	if err := os.WriteFile(releaseWorkflowPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", releaseWorkflowPath, err)
	}
	cli.Success("Generated %s", releaseWorkflowPath)

	warning, err := releaseRootWarning()
	if err != nil {
		return err
	}
	if warning != "" {
		cli.Warning("%s", warning)
	}
	return nil
}

// releaseRootWarning returns a warning when the project (the current
// directory) is inside a git repository but not its root: GitHub runs only
// the root's workflows. Outside a repository there is nothing to say.
func releaseRootWarning() (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", nil // not a git repository (yet)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("resolving the repository root: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if wd, err = filepath.EvalSymlinks(wd); err != nil {
		return "", err
	}
	if wd == root {
		return "", nil
	}
	return fmt.Sprintf("%s is in %s, not at the repository root %s — GitHub won't run it", releaseWorkflowPath, wd, root), nil
}
