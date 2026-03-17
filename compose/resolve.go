// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package compose

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// SchemafExtension holds the x-schemaf metadata from a compose file
type SchemafExtension struct {
	Project                string            `yaml:"project"`
	ShortName              string            `yaml:"short-name"`
	DependsOnCompose       []string          `yaml:"depends-on-compose"`
	Health                 *HealthConfig     `yaml:"health"`
	NativeStop             string            `yaml:"native-stop"`
	DevInstructions        string            `yaml:"dev-instructions"`
	EnvOverridesWhenAbsent map[string]string `yaml:"env-overrides-when-absent"`
	// Database configuration for dev/test environments
	DBPort    int    `yaml:"db-port"`     // host port the postgres service is mapped to
	DevDBPass string `yaml:"dev-db-pass"` // password used in dev/test (default: "dev")
}

// HealthConfig describes how to health-check a service
type HealthConfig struct {
	Type string `yaml:"type"` // "http" or "redis_ping"
	Path string `yaml:"path"` // for http
}

// ComposeFile represents a parsed compose file with its metadata
type ComposeFile struct {
	Path     string // absolute path
	Dir      string // directory containing the file
	Schemaf  *SchemafExtension
	Services map[string]ServiceDef
}

// ServiceDef holds the parts of a compose service we care about
type ServiceDef struct {
	ContainerName string
	Schemaf       *SchemafExtension
}

// rawCompose is used only for YAML parsing
type rawCompose struct {
	Services map[string]rawService  `yaml:"services"`
	XSchemaf map[string]interface{} `yaml:"x-schemaf"`
}

type rawService struct {
	ContainerName string                 `yaml:"container_name"`
	XSchemaf      map[string]interface{} `yaml:"x-schemaf"`
}

// Resolve walks the dependency graph starting from the given compose file paths.
// Returns an ordered list of compose file paths (dependencies before dependents),
// deduplicated.
func Resolve(entryPaths []string) ([]*ComposeFile, error) {
	seen := map[string]bool{}
	var ordered []*ComposeFile

	for _, p := range entryPaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", p, err)
		}
		if err := walk(abs, seen, &ordered); err != nil {
			return nil, err
		}
	}

	return ordered, nil
}

func walk(absPath string, seen map[string]bool, ordered *[]*ComposeFile) error {
	if seen[absPath] {
		return nil
	}
	seen[absPath] = true

	cf, err := parseComposeFile(absPath)
	if err != nil {
		return err
	}

	// Walk dependencies first (depth-first, deps before dependents)
	if cf.Schemaf != nil {
		for _, dep := range cf.Schemaf.DependsOnCompose {
			depAbs := dep
			if !filepath.IsAbs(dep) {
				depAbs = filepath.Join(cf.Dir, dep)
			}
			depAbs = filepath.Clean(depAbs)
			if err := walk(depAbs, seen, ordered); err != nil {
				return err
			}
		}
	}

	*ordered = append(*ordered, cf)
	return nil
}

func parseComposeFile(absPath string) (*ComposeFile, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", absPath, err)
	}

	var raw rawCompose
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", absPath, err)
	}

	cf := &ComposeFile{
		Path:     absPath,
		Dir:      filepath.Dir(absPath),
		Services: make(map[string]ServiceDef),
	}

	// Parse top-level x-schemaf
	if raw.XSchemaf != nil {
		schemaf, err := parseSchemafExtension(raw.XSchemaf)
		if err != nil {
			return nil, fmt.Errorf("parsing x-schemaf in %q: %w", absPath, err)
		}
		cf.Schemaf = schemaf
	}

	// Parse per-service info
	for name, svc := range raw.Services {
		sd := ServiceDef{ContainerName: svc.ContainerName}
		if svc.XSchemaf != nil {
			schemaf, err := parseSchemafExtension(svc.XSchemaf)
			if err != nil {
				return nil, fmt.Errorf("parsing x-schemaf for service %q in %q: %w", name, absPath, err)
			}
			sd.Schemaf = schemaf
		}
		cf.Services[name] = sd
	}

	return cf, nil
}

// parseSchemafExtension converts a raw map[string]interface{} (from YAML) into SchemafExtension.
// We re-marshal/unmarshal via YAML to avoid manual field mapping.
func parseSchemafExtension(raw map[string]interface{}) (*SchemafExtension, error) {
	b, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var schemaf SchemafExtension
	if err := yaml.Unmarshal(b, &schemaf); err != nil {
		return nil, err
	}
	return &schemaf, nil
}

// ShortNameMap builds a map of short-name → service name across all resolved compose files.
// The schemaf extension on the compose file level defines a short name for the whole file's
// primary service; per-service x-schemaf.short-name overrides for individual services.
func ShortNameMap(files []*ComposeFile) map[string]string {
	m := map[string]string{} // short-name → compose service name
	for _, cf := range files {
		for svcName, svc := range cf.Services {
			if svc.Schemaf != nil && svc.Schemaf.ShortName != "" {
				m[svc.Schemaf.ShortName] = svcName
			}
		}
		// Top-level x-schemaf short-name applies to the file's first/only service
		// (useful for single-service compose files)
		if cf.Schemaf != nil && cf.Schemaf.ShortName != "" {
			// If only one service, map it
			if len(cf.Services) == 1 {
				for svcName := range cf.Services {
					if _, already := m[cf.Schemaf.ShortName]; !already {
						m[cf.Schemaf.ShortName] = svcName
					}
				}
			}
		}
	}
	return m
}

// AllServices returns a flat list of all compose service names across all files.
func AllServices(files []*ComposeFile) []string {
	var names []string
	seen := map[string]bool{}
	for _, cf := range files {
		for name := range cf.Services {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// FilePaths returns just the paths from a list of ComposeFiles.
func FilePaths(files []*ComposeFile) []string {
	paths := make([]string, len(files))
	for i, cf := range files {
		paths[i] = cf.Path
	}
	return paths
}

// FindSchemafByService looks up the SchemafExtension for a given compose service name.
func FindSchemafByService(files []*ComposeFile, svcName string) *SchemafExtension {
	for _, cf := range files {
		if svc, ok := cf.Services[svcName]; ok && svc.Schemaf != nil {
			return svc.Schemaf
		}
		// Fall back to file-level schemaf if only one service
		if len(cf.Services) == 1 {
			if _, ok := cf.Services[svcName]; ok && cf.Schemaf != nil {
				return cf.Schemaf
			}
		}
	}
	return nil
}

// ContainerName returns the docker container_name for a compose service.
func ContainerName(files []*ComposeFile, svcName string) string {
	for _, cf := range files {
		if svc, ok := cf.Services[svcName]; ok && svc.ContainerName != "" {
			return svc.ContainerName
		}
	}
	return svcName // fallback
}
