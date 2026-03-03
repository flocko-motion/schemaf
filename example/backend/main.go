package main

import (
	"context"
	"embed"
	"log"

	"atlas.local/base/atlas"
	_ "atlas.local/example/backend/api"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	app := atlas.New(context.Background(), "atlas-example")
	app.AddMigrations(migrations)
	log.Fatal(app.Run())
}
