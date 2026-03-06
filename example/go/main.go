package main

import (
	"context"
	"log"

	"schemaf.local/base/schemaf"

	// Generated providers — regenerate with: ./codegen.sh
	// "schemaf.local/example/api"
	// "schemaf.local/example/db"
)

func main() {
	ctx := context.Background()
	app := schemaf.New(ctx, "schemaf-example")

	// Wire up generated providers:
	// app.AddDb(db.Provider)
	// app.AddApi(api.Provider)

	log.Fatal(app.Run())
}
