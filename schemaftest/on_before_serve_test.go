package schemaftest

import (
	"context"
	"testing"

	"github.com/flocko-motion/schemaf/db"
	"github.com/flocko-motion/schemaf/schemaf"
)

// An OnBeforeServe hook registered in Wire runs in schemaftest.Start on the
// migrated database, before the first request: the data it seeds is there
// without polling.
func TestOnBeforeServeSeedsBeforeFirstRequest(t *testing.T) {
	requireContainers(t)
	inst := Start(t, func(app *schemaf.App) {
		wire(app)
		app.OnBeforeServe(func(ctx context.Context) error {
			_, err := db.DB().ExecContext(ctx, `INSERT INTO widgets (name) VALUES ('seeded')`)
			return err
		})
	})
	if code, body := get(t, inst.URL()+"/api/widgets/count", nil); code != 200 || body != "1" {
		t.Fatalf("first request sees %d %q, want 200 \"1\"", code, body)
	}
}
