// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"fmt"
	"strconv"
)

// The generated compose files read a dev instance's names and Postgres port
// from these variables (compose.gen.yml.tmpl, compose.dev.yml.tmpl). Unset,
// they default to the default instance's, so compose without them is exactly
// the default stack (INSTALL.md › Parallel dev instances).
const (
	// InstanceSuffixEnv is appended to the project name in the compose
	// project, container, network and volume names: "" or "-<n>".
	InstanceSuffixEnv = "SCHEMAF_INSTANCE_SUFFIX"
	// PostgresPortEnv is Postgres's host port in the dev overlay.
	PostgresPortEnv = "SCHEMAF_POSTGRES_PORT"
)

// InstanceComposeEnv returns the compose variables for dev instance n
// (0 = default) whose Postgres listens on postgresPort.
func InstanceComposeEnv(n, postgresPort int) []string {
	suffix := ""
	if n != 0 {
		suffix = fmt.Sprintf("-%d", n)
	}
	return []string{
		InstanceSuffixEnv + "=" + suffix,
		PostgresPortEnv + "=" + strconv.Itoa(postgresPort),
	}
}
