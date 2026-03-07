package main

import (
	"context"
	"log"

	"github.com/flocko-motion/schemaf/schemaf"

	// Generated providers — regenerate with: ./codegen.sh
	"schemaf.local/example/api"
	// "schemaf.local/example/db"
)

func main() {
	ctx := context.Background()
	app := schemaf.New(ctx, "schemaf-example")

	app.AddApi(api.Provider)
	// app.AddDb(db.Provider)

	log.Fatal(app.Run())
}
