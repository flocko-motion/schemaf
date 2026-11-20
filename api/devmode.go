// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import "os"

// DevMode reports whether the server runs in development mode.
//
// Development mode is opt-in: schemaf.sh sets SCHEMAF_DEV=1 for every command
// that loads the dev env file (./schemaf.sh dev, auth token, project
// subcommands). ./schemaf.sh run, the generated compose, a built image and a
// bare ./myapp start in production. Any SCHEMAF_DEV value other than "1" or
// empty is refused when the app starts, so DevMode never sees one.
//
// Gate dev-only behaviour on it:
//
//	if api.DevMode() {
//	    // dev-only endpoints, mock services, preseed
//	}
func DevMode() bool {
	return os.Getenv("SCHEMAF_DEV") == "1"
}

// modeName is the mode as reported by /api/status.
func modeName() string {
	if DevMode() {
		return "development"
	}
	return "production"
}
