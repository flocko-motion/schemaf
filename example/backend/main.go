package main

// ⚠️ SPECIFICATION-FIRST PROJECT
//
// This is a REFERENCE IMPLEMENTATION showing the target architecture per README.
// The README is the source of truth for schemaf design.
//
// DO NOT modify this architecture without first:
// 1. Discussing the change with the user
// 2. Updating the README documentation
// 3. Getting approval for the architectural change
//
// All design decisions are made through pair programming and documented in README first.

import (
	"context"
	"log"

	"schemaf.local/base/schemaf"
	// REFERENCE: Import generated providers (README design - not yet implemented)
	// "schemaf.local/example/go/api"
	// "schemaf.local/example/go/db"
)

func main() {
	ctx := context.Background()
	app := schemaf.New(ctx, "schemaf-example") // TODO(framework): remove appName param, should only take ctx (README design)

	// REFERENCE IMPLEMENTATION (README design - not yet implemented):
	// Wire up generated providers (pass as function references, not calls!):
	// - db.Provider returns migrations + query config (from db/migrations.gen.go)
	// - api.Provider returns handler registrations (from api/endpoints.gen.go)
	//
	// app.AddDb(db.Provider)     // Note: no () - pass function reference!
	// app.AddApi(api.Provider)   // Note: no () - pass function reference!

	// Optional: mount custom CLI commands
	// app.AddSubcommand("custom", customSubcommandProvider)

	// Optional: register background services
	// IMPORTANT: Use AddService(), NOT go routines!
	// Services only start when running "server" or "dev" command.
	// This keeps "codegen", "compose", etc. fast and clean.
	//
	// app.AddService(worker.ServiceProvider)
	// app.AddService(scheduler.ServiceProvider)

	// REFERENCE: app.Run() hands over to Cobra - CLI command routing happens here
	log.Fatal(app.Run())
}
