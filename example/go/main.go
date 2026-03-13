// main.go — Application entry point. Wires up generated providers and starts the app.
// See INSTALL.md for setup and EXTEND.md for adding providers.
// Rule: only add app.Add*() calls here — all logic belongs in provider packages.
package main

import (
	"context"
	"log"

	"github.com/flocko-motion/schemaf/schemaf"

	"schemaf.local/example/api"
	"schemaf.local/example/db"
)

func main() {
	ctx := context.Background()
	app := schemaf.New(ctx)

	app.AddApi(api.Provider)
	app.AddDb(db.Provider)

	log.Fatal(app.Run())
}
