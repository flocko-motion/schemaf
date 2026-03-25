// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
	"gopkg.in/yaml.v3"
)

//go:embed compose.gen.yml.tmpl
var composeGenTemplate string

//go:embed compose.dev.yml.tmpl
var composeDevTemplate string

//go:embed compose.test.yml.tmpl
var composeTestTemplate string

//go:embed test-env.sh.tmpl
var testEnvShTemplate string

//go:embed Dockerfile.tmpl
var dockerfileTemplate string

//go:embed schemaf.sh.tmpl
var schemafShTemplate string

func newComposeCmd(_ *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "compose",
		Short: "Generate compose.gen.yml",
		Long: `Generates compose.gen.yml from the framework template.

Includes postgres + backend services. Any *.yml files in compose/ are
included via Docker Compose include directives.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runComposeGen()
		},
	}
}

func runComposeGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}

	extensions, err := scanComposeExtensions()
	if err != nil {
		return err
	}

	extServices, err := scanExtServices(extensions)
	if err != nil {
		return err
	}

	_, hasFrontend := os.Stat("frontend")
	data := map[string]any{"Name": name, "Extensions": extensions, "ExtServices": extServices, "HasFrontend": hasFrontend == nil}

	if err := renderTemplate(composeGenTemplate, "compose.gen.yml", data); err != nil {
		return err
	}
	cli.Success("Generated compose.gen.yml (project: %s)", name)

	if err := renderTemplate(composeDevTemplate, "compose.dev.gen.yml", data); err != nil {
		return err
	}
	cli.Success("Generated compose.dev.gen.yml")

	if err := renderTemplate(composeTestTemplate, "compose.test.gen.yml", data); err != nil {
		return err
	}
	cli.Success("Generated compose.test.gen.yml")

	if err := renderTemplate(dockerfileTemplate, "Dockerfile.gen", data); err != nil {
		return err
	}
	cli.Success("Generated Dockerfile.gen")

	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}

	if err := renderTemplate(testEnvShTemplate, "gen/test-env.sh", data); err != nil {
		return err
	}
	if err := os.Chmod("gen/test-env.sh", 0755); err != nil {
		return fmt.Errorf("chmod gen/test-env.sh: %w", err)
	}
	cli.Success("Generated gen/test-env.sh")

	if err := renderTemplate(schemafShTemplate, "schemaf.sh", data); err != nil {
		return err
	}
	if err := os.Chmod("schemaf.sh", 0755); err != nil {
		return fmt.Errorf("chmod schemaf.sh: %w", err)
	}
	cli.Success("Generated schemaf.sh")

	return nil
}

func renderTemplate(tmplStr, outPath string, data map[string]any) error {
	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", outPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template %s: %w", outPath, err)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}
	return nil
}

// scanComposeExtensions returns relative paths to *.yml files in compose/.
func scanComposeExtensions() ([]string, error) {
	entries, err := os.ReadDir("compose")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading compose/: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yml" {
			paths = append(paths, "compose/"+e.Name())
		}
	}
	return paths, nil
}

// ExtService describes a project compose extension service that exposes internal ports.
// The codegen adds host port mappings for these services in compose.test.yml so that
// tests running on the host can reach them.
type ExtService struct {
	Name   string   // service name, e.g. "clock"
	Ports  []string // internal ports declared via expose:, e.g. ["8080"]
	EnvVar string   // env var name, e.g. "CLOCK_URL"
}

// scanExtServices parses compose/ extension files and returns services with expose: entries.
func scanExtServices(extensions []string) ([]ExtService, error) {
	type composeFile struct {
		Services map[string]struct {
			Expose []string `yaml:"expose"`
		} `yaml:"services"`
	}

	var result []ExtService
	for _, path := range extensions {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var cf composeFile
		if err := yaml.Unmarshal(data, &cf); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		for name, svc := range cf.Services {
			if len(svc.Expose) == 0 {
				continue
			}
			result = append(result, ExtService{
				Name:   name,
				Ports:  svc.Expose,
				EnvVar: strings.ToUpper(name) + "_URL",
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
