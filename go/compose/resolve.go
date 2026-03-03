package compose

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// AtlasExtension holds the x-atlas metadata from a compose file
type AtlasExtension struct {
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
	Atlas    *AtlasExtension
	Services map[string]ServiceDef
}

// ServiceDef holds the parts of a compose service we care about
type ServiceDef struct {
	ContainerName string
	Atlas         *AtlasExtension
}

// rawCompose is used only for YAML parsing
type rawCompose struct {
	Services map[string]rawService  `yaml:"services"`
	XAtlas   map[string]interface{} `yaml:"x-atlas"`
}

type rawService struct {
	ContainerName string                 `yaml:"container_name"`
	XAtlas        map[string]interface{} `yaml:"x-atlas"`
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
	if cf.Atlas != nil {
		for _, dep := range cf.Atlas.DependsOnCompose {
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

	// Parse top-level x-atlas
	if raw.XAtlas != nil {
		atlas, err := parseAtlasExtension(raw.XAtlas)
		if err != nil {
			return nil, fmt.Errorf("parsing x-atlas in %q: %w", absPath, err)
		}
		cf.Atlas = atlas
	}

	// Parse per-service info
	for name, svc := range raw.Services {
		sd := ServiceDef{ContainerName: svc.ContainerName}
		if svc.XAtlas != nil {
			atlas, err := parseAtlasExtension(svc.XAtlas)
			if err != nil {
				return nil, fmt.Errorf("parsing x-atlas for service %q in %q: %w", name, absPath, err)
			}
			sd.Atlas = atlas
		}
		cf.Services[name] = sd
	}

	return cf, nil
}

// parseAtlasExtension converts a raw map[string]interface{} (from YAML) into AtlasExtension.
// We re-marshal/unmarshal via YAML to avoid manual field mapping.
func parseAtlasExtension(raw map[string]interface{}) (*AtlasExtension, error) {
	b, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var atlas AtlasExtension
	if err := yaml.Unmarshal(b, &atlas); err != nil {
		return nil, err
	}
	return &atlas, nil
}

// ShortNameMap builds a map of short-name → service name across all resolved compose files.
// The atlas extension on the compose file level defines a short name for the whole file's
// primary service; per-service x-atlas.short-name overrides for individual services.
func ShortNameMap(files []*ComposeFile) map[string]string {
	m := map[string]string{} // short-name → compose service name
	for _, cf := range files {
		for svcName, svc := range cf.Services {
			if svc.Atlas != nil && svc.Atlas.ShortName != "" {
				m[svc.Atlas.ShortName] = svcName
			}
		}
		// Top-level x-atlas short-name applies to the file's first/only service
		// (useful for single-service compose files)
		if cf.Atlas != nil && cf.Atlas.ShortName != "" {
			// If only one service, map it
			if len(cf.Services) == 1 {
				for svcName := range cf.Services {
					if _, already := m[cf.Atlas.ShortName]; !already {
						m[cf.Atlas.ShortName] = svcName
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

// FindAtlasByService looks up the AtlasExtension for a given compose service name.
func FindAtlasByService(files []*ComposeFile, svcName string) *AtlasExtension {
	for _, cf := range files {
		if svc, ok := cf.Services[svcName]; ok && svc.Atlas != nil {
			return svc.Atlas
		}
		// Fall back to file-level atlas if only one service
		if len(cf.Services) == 1 {
			if _, ok := cf.Services[svcName]; ok && cf.Atlas != nil {
				return cf.Atlas
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
