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

func runComposeGen() error {
	name, err := readProjectName()
	if err != nil {
		return err
	}
	port, err := readPort()
	if err != nil {
		return err
	}

	sharedExts, devExts, err := scanComposeExtensions()
	if err != nil {
		return err
	}
	allExts := append(sharedExts, devExts...)

	sharedServices, err := scanExtServices(sharedExts)
	if err != nil {
		return err
	}
	allServices, err := scanExtServices(allExts)
	if err != nil {
		return err
	}

	_, hasFrontend := os.Stat("frontend")
	base := map[string]any{
		"Name":         name,
		"HasFrontend":  hasFrontend == nil,
		"Port":         port,
		"FrontendPort": port + 2,
		"PostgresPort": port + 3,
		"GoVersion":    readGoVersion(),
	}

	// Prod: shared extensions only.
	prodData := copyMap(base)
	prodData["Extensions"] = sharedExts
	prodData["ExtServices"] = sharedServices
	if err := renderTemplate(composeGenTemplate, "compose.gen.yml", prodData); err != nil {
		return err
	}
	cli.Success("Generated compose.gen.yml (project: %s)", name)

	// Dev: dev-only extensions (shared are already in compose.gen.yml base).
	devData := copyMap(base)
	devData["Extensions"] = devExts
	devData["ExtServices"] = allServices
	if err := renderTemplate(composeDevTemplate, "compose.dev.gen.yml", devData); err != nil {
		return err
	}
	cli.Success("Generated compose.dev.gen.yml")

	// Test: shared + dev-only extensions, without Postgres.
	testServices, err := checkTestStack(allExts)
	if err != nil {
		return err
	}
	testData := copyMap(base)
	testData["Extensions"] = allExts
	testData["ExtServices"] = allServices
	if err := renderTemplate(composeTestTemplate, "compose.test.gen.yml", testData); err != nil {
		return err
	}
	cli.Success("Generated compose.test.gen.yml")

	if err := renderTemplate(dockerfileTemplate, "Dockerfile.gen", base); err != nil {
		return err
	}
	cli.Success("Generated Dockerfile.gen")

	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}

	if err := renderTemplate(testEnvShTemplate, "gen/test-env.sh", testData); err != nil {
		return err
	}
	if err := os.Chmod("gen/test-env.sh", 0755); err != nil {
		return fmt.Errorf("chmod gen/test-env.sh: %w", err)
	}
	cli.Success("Generated gen/test-env.sh")

	shBase := copyMap(base)
	shBase["HasTestStack"] = testServices > 0
	shData, err := schemafShData(shBase)
	if err != nil {
		return err
	}
	if err := renderTemplate(schemafShTemplate, "schemaf.sh", shData); err != nil {
		return err
	}
	if err := os.Chmod("schemaf.sh", 0755); err != nil {
		return fmt.Errorf("chmod schemaf.sh: %w", err)
	}
	cli.Success("Generated schemaf.sh")

	return nil
}

// schemafShData adds what schemaf.sh.tmpl needs beyond the compose data: the
// schemaf version (for the banner, so no Go runs at startup) and the inputs
// hash definition (inputGlobs / inputExcludes), rendered into shell syntax.
func schemafShData(base map[string]any) (map[string]any, error) {
	version, err := readSchemafVersion()
	if err != nil {
		return nil, err
	}
	d := copyMap(base)
	d["SchemafVersion"] = version
	d["InputGlobs"] = strings.Join(inputGlobs, " ")
	d["InputExcludes"] = strings.Join(inputExcludes, "|")
	d["SchemaSourceExcludes"] = strings.Join(schemaSourceExcludes, "|")
	name, _ := base["Name"].(string)
	if name == "" {
		return nil, fmt.Errorf("schemaf.sh needs the project name — set name in schemaf.toml")
	}
	for k, v := range volumeConflictShell(name) {
		d[k] = v
	}
	return d, nil
}

// volumeConflictShell renders the Go volume-conflict refusal
// (cli.VolumeConflictMessage) for _schemaf_check_volume, so shell and Go print
// the same text. The runtime-dependent parts become the shell variables
// ${rt}, ${other}, ${both} and ${hint}:
//
//	VolumeConflict      the message, for an unquoted heredoc
//	VolumeConflictBoth  the value of both after its leading newline, for double quotes
//	VolumeHintPodman    the value of hint when the other runtime is podman
//	VolumeHintDocker    … when it is docker
func volumeConflictShell(project string) map[string]string {
	const rt, other, both, hint = "\x00rt\x00", "\x00other\x00", "\x00both\x00", "\x00hint\x00"
	vars := strings.NewReplacer(rt, "${rt}", other, "${other}", both, "${both}", hint, "${hint}")
	heredoc := strings.NewReplacer(`\`, `\\`, "$", `\$`, "`", "\\`")
	dquote := strings.NewReplacer(`\`, `\\`, "$", `\$`, "`", "\\`", `"`, `\"`)
	return map[string]string{
		"VolumeConflict":     vars.Replace(heredoc.Replace(cli.VolumeConflictText(rt, other, project, both, hint))),
		"VolumeConflictBoth": vars.Replace(dquote.Replace(strings.TrimPrefix(cli.VolumeConflictBoth(rt, project), "\n"))),
		"VolumeHintPodman":   dquote.Replace(cli.VolumeConflictSwitchHint(cli.Podman)),
		"VolumeHintDocker":   dquote.Replace(cli.VolumeConflictSwitchHint(cli.Docker)),
	}
}

func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
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

// scanComposeExtensions returns relative paths to *.yml files in compose/,
// split into shared (all environments) and dev-only (*.dev.yml).
func scanComposeExtensions() (shared, devOnly []string, err error) {
	entries, err := os.ReadDir("compose")
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading compose/: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yml" {
			continue
		}
		path := "compose/" + e.Name()
		if strings.HasSuffix(e.Name(), ".dev.yml") {
			devOnly = append(devOnly, path)
		} else {
			shared = append(shared, path)
		}
	}
	return shared, devOnly, nil
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

// checkTestStack returns how many services the test stack (extensions)
// declares. The test stack has no Postgres, so a service depending on
// postgres is an error naming it.
func checkTestStack(extensions []string) (int, error) {
	type composeFile struct {
		Services map[string]struct {
			DependsOn yaml.Node `yaml:"depends_on"`
		} `yaml:"services"`
	}
	n := 0
	var problems []string
	for _, path := range extensions {
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, fmt.Errorf("reading %s: %w", path, err)
		}
		var cf composeFile
		if err := yaml.Unmarshal(data, &cf); err != nil {
			return 0, fmt.Errorf("parsing %s: %w", path, err)
		}
		n += len(cf.Services)
		for name, svc := range cf.Services {
			if dependsOn(svc.DependsOn, "postgres") {
				problems = append(problems, fmt.Sprintf("service %s in %s depends on postgres", name, path))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return 0, fmt.Errorf("the test stack (compose.test.gen.yml) has no postgres: tests get their databases from the test harness (EXTEND.md › Testing). Remove the dependency, or move the service to a file the test stack doesn't include:\n  - %s",
			strings.Join(problems, "\n  - "))
	}
	return n, nil
}

// dependsOn reports whether a depends_on node (a list of names, or a map
// keyed by name) names service.
func dependsOn(node yaml.Node, service string) bool {
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Value == service {
				return true
			}
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == service {
				return true
			}
		}
	}
	return false
}
