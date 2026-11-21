package schemaftest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/schemaf"
)

// appCaller is the test app's own caller type; only its OnRequest hook
// creates one.
type appCaller struct{ name string }

type ctxKeyAppCaller struct{}

// whoamiEndpoint answers the app caller the hook resolved.
type whoamiEndpoint struct{}

func (whoamiEndpoint) Method() string      { return "GET" }
func (whoamiEndpoint) Path() string        { return "/api/whoami" }
func (whoamiEndpoint) Auth() api.AuthLevel { return api.AuthOptional }
func (whoamiEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
	c, ok := r.Context().Value(ctxKeyAppCaller{}).(appCaller)
	if !ok {
		return fmt.Errorf("no app caller in the context")
	}
	fmt.Fprint(w, c.name)
	return nil
}

// hookSpec is testSpec plus /api/whoami.
var hookSpec = strings.Replace(testSpec, `"/api/widgets/count":{"get":{}}`,
	`"/api/widgets/count":{"get":{}},
"/api/whoami":{"get":{}}`, 1)

// hookWire is the test app's Wire plus /api/whoami and an OnRequest hook: it resolves the
// subject to an appCaller, refusing "mallory" as an incomplete account.
func hookWire(app *schemaf.App) {
	wire(app)
	app.AddApi(func() { // runs after wire's provider: replaces its spec
		api.SetOpenAPISpec(hookSpec)
		api.Register(api.NewRawRoute(whoamiEndpoint{}, "", ""))
	})
	app.OnRequest(func(ctx context.Context, a *api.Admission) (context.Context, error) {
		subject, ok := a.Subject()
		switch {
		case !ok:
			return context.WithValue(ctx, ctxKeyAppCaller{}, appCaller{"guest"}), nil
		case subject == "mallory":
			return nil, api.Error(api.ErrForbidden, "account_incomplete", "finish your registration first")
		}
		return context.WithValue(ctx, ctxKeyAppCaller{}, appCaller{"user:" + subject}), nil
	})
}

func TestOnRequestThroughWire(t *testing.T) {
	requireContainers(t)
	inst := Start(t, hookWire)

	if code, body := get(t, inst.URL()+"/api/whoami", nil); code != 200 || body != "guest" {
		t.Errorf("anonymous: %d %q, want 200 \"guest\"", code, body)
	}
	if code, body := get(t, inst.URL()+"/api/whoami", inst.AuthHeader(inst.Token("alice"))); code != 200 || body != "user:alice" {
		t.Errorf("alice: %d %q, want 200 \"user:alice\"", code, body)
	}
	code, body := get(t, inst.URL()+"/api/whoami", inst.AuthHeader(inst.Token("mallory")))
	if want := `{"error":"finish your registration first","code":"account_incomplete"}`; code != 403 || body != want {
		t.Errorf("mallory: %d %s, want 403 %s", code, body, want)
	}
	// schemaf's own routes run the hook too.
	if code, _ := get(t, inst.URL()+"/api/user/me", inst.AuthHeader(inst.Token("mallory"))); code != 403 {
		t.Errorf("mallory on /api/user/me: %d, want 403", code)
	}
}
