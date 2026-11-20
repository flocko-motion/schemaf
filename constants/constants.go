// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package constants

// projectName is set by the generated constants.gen.go init() function.
var projectName string

// port is the base port from schemaf.toml. Default 8000.
var port = 8000

// SetProjectName is called from the generated constants.gen.go to register
// the project name at init time.
func SetProjectName(name string) {
	projectName = name
}

// ProjectName returns the registered project name.
func ProjectName() string {
	if projectName == "" {
		panic("project name not set: did you forget to run codegen?")
	}
	return projectName
}

// IsProjectNameSet reports whether a project name has been registered (either
// compiled in via constants.gen.go, or loaded from schemaf.toml at runtime).
func IsProjectNameSet() bool { return projectName != "" }

// SetPort is called from the generated constants.gen.go to register
// the base port at init time.
func SetPort(p int) {
	port = p
}

// ConfigPort returns the base port from schemaf.toml, without the instance offset.
func ConfigPort() int { return port }

// Port returns the base port of the current instance (backend server):
// port + 10 × instance, i.e. the configured port for the default instance.
func Port() int { return port + InstancePortStep*instance }

// FrontendPort returns the frontend dev server port (base + 2).
func FrontendPort() int { return Port() + 2 }

// PostgresPort returns the Postgres host port (base + 3).
func PostgresPort() int { return Port() + 3 }
