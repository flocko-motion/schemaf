// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve registers routes on a fresh registry and returns a test server.
func serve(t *testing.T, routes ...Route) *httptest.Server {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	for _, r := range routes {
		Register(r)
	}
	srv := httptest.NewServer(NewMux())
	t.Cleanup(srv.Close)
	return srv
}

// do sends a request and returns the response and its body.
func do(t *testing.T, method, url, body string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// --- Request headers ---

type langResp struct {
	Lang string `json:"lang"`
}

type langEndpoint struct{}

func (langEndpoint) Method() string  { return "GET" }
func (langEndpoint) Path() string    { return "/test/lang" }
func (langEndpoint) Auth() AuthLevel { return AuthNone }
func (langEndpoint) Handle(ctx context.Context, _ struct{}) (langResp, error) {
	return langResp{Lang: Header(ctx, "accept-language")}, nil
}

func TestHeaderReadsRequestHeaderInHandle(t *testing.T) {
	srv := serve(t, NewRoute[struct{}, langResp](langEndpoint{}, "", ""))
	_, body := do(t, "GET", srv.URL+"/test/lang", "", map[string]string{"Accept-Language": "de-DE"})
	if want := `{"lang":"de-DE"}` + "\n"; body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	_, body = do(t, "GET", srv.URL+"/test/lang", "", nil)
	if want := `{"lang":""}` + "\n"; body != want {
		t.Fatalf("without header: body = %q, want %q", body, want)
	}
}

func TestHeaderOutsideRequestIsEmpty(t *testing.T) {
	if got := Header(context.Background(), "Accept-Language"); got != "" {
		t.Fatalf("Header(Background) = %q, want empty", got)
	}
}

// --- Starting a session ---

type redeemReq struct {
	User string `json:"user"`
	Fail bool   `json:"fail"`
}

type redeemResp struct {
	Token string `json:"token"`
}

type redeemEndpoint struct{}

func (redeemEndpoint) Method() string  { return "POST" }
func (redeemEndpoint) Path() string    { return "/test/redeem" }
func (redeemEndpoint) Auth() AuthLevel { return AuthNone }
func (redeemEndpoint) Handle(ctx context.Context, req redeemReq) (redeemResp, error) {
	tok, err := StartSessionCtx(ctx, req.User)
	if err != nil {
		return redeemResp{}, err
	}
	if req.Fail {
		return redeemResp{}, ErrConflict
	}
	return redeemResp{Token: tok}, nil
}

func TestStartSessionCtxSetsCookieOnTypedResponse(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	Register(NewRoute[redeemReq, redeemResp](redeemEndpoint{}, "", ""))
	e := newAuthEnv(t)

	resp := e.do("POST", "/test/redeem", reqOpts{body: `{"user":"guest-1"}`})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body redeemResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(resp)
	if c == nil || c.Value != body.Token {
		t.Fatalf("cookie = %+v, token = %q", c, body.Token)
	}
	if got := meSubject(t, e.do("GET", "/api/user/me", reqOpts{cookie: c.Value})); got != "guest-1" {
		t.Errorf("subject = %q, want guest-1", got)
	}

	// A handler error after StartSessionCtx still carries the cookie.
	resp = e.do("POST", "/test/redeem", reqOpts{body: `{"user":"guest-2","fail":true}`})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", resp.StatusCode)
	}
	if sessionCookie(resp) == nil {
		t.Error("error response after StartSessionCtx has no session cookie")
	}
}

func TestStartSessionCtxOutsideEndpointFails(t *testing.T) {
	e := newAuthEnv(t)
	_, err := StartSessionCtx(context.Background(), "guest-1")
	if err == nil || !strings.Contains(err.Error(), "StartSession(") {
		t.Fatalf("err = %v, want an error naming StartSession(ctx, w, subject)", err)
	}
	if len(e.store.sessions) != 0 {
		t.Errorf("a session was created: %v", e.store.sessions)
	}
}

// --- Content types ---

type gameReq struct {
	ID string `path:"id"`
}

type game struct {
	Title    string   `json:"title"`
	MaxUsers int      `json:"maxUsers"`
	Tags     []string `json:"tags"`
}

// getGameEndpoint produces YAML first, JSON second.
type getGameEndpoint struct{}

func (getGameEndpoint) Method() string     { return "GET" }
func (getGameEndpoint) Path() string       { return "/test/games/{id}" }
func (getGameEndpoint) Auth() AuthLevel    { return AuthNone }
func (getGameEndpoint) Produces() []string { return []string{"application/x-yaml", "application/json"} }
func (getGameEndpoint) Handle(_ context.Context, req gameReq) (game, error) {
	if req.ID == "missing" {
		return game{}, ErrNotFound
	}
	return game{Title: "Quest " + req.ID, MaxUsers: 4, Tags: []string{"a", "b"}}, nil
}

// putGameEndpoint consumes YAML first, JSON second, and echoes JSON.
type putGameEndpoint struct{}

func (putGameEndpoint) Method() string     { return "PUT" }
func (putGameEndpoint) Path() string       { return "/test/games" }
func (putGameEndpoint) Auth() AuthLevel    { return AuthNone }
func (putGameEndpoint) Consumes() []string { return []string{"application/x-yaml", "application/json"} }
func (putGameEndpoint) Handle(_ context.Context, req game) (game, error) {
	return req, nil
}

func gameServer(t *testing.T) *httptest.Server {
	return serve(t,
		NewRoute[gameReq, game](getGameEndpoint{}, "", ""),
		NewRoute[game, game](putGameEndpoint{}, "", ""),
	)
}

const questYAML = "title: Quest 7\nmaxUsers: 4\ntags:\n    - a\n    - b\n"
const questJSON = `{"title":"Quest 7","maxUsers":4,"tags":["a","b"]}` + "\n"

func TestProducesNegotiatesByAccept(t *testing.T) {
	srv := gameServer(t)
	for _, tc := range []struct {
		accept, wantType, wantBody string
	}{
		{"", "application/x-yaml", questYAML},
		{"*/*", "application/x-yaml", questYAML},
		{"application/json", "application/json", questJSON},
		{"application/x-yaml", "application/x-yaml", questYAML},
		{"application/x-yaml;q=0.5, application/json", "application/json", questJSON},
		{"text/html, application/*;q=0.9", "application/x-yaml", questYAML},
	} {
		h := map[string]string{}
		if tc.accept != "" {
			h["Accept"] = tc.accept
		}
		resp, body := do(t, "GET", srv.URL+"/test/games/7", "", h)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Accept %q: status %d", tc.accept, resp.StatusCode)
			continue
		}
		if got := resp.Header.Get("Content-Type"); got != tc.wantType {
			t.Errorf("Accept %q: Content-Type %q, want %q", tc.accept, got, tc.wantType)
		}
		if body != tc.wantBody {
			t.Errorf("Accept %q: body %q, want %q", tc.accept, body, tc.wantBody)
		}
	}
}

func TestProducesNoMatchIs406(t *testing.T) {
	srv := gameServer(t)
	resp, body := do(t, "GET", srv.URL+"/test/games/7", "", map[string]string{"Accept": "text/html"})
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("status %d, want 406", resp.StatusCode)
	}
	if !strings.Contains(body, `"code":"not_acceptable"`) {
		t.Errorf("body %q", body)
	}
}

func TestErrorsAreJSONWhateverAccept(t *testing.T) {
	srv := gameServer(t)
	resp, body := do(t, "GET", srv.URL+"/test/games/missing", "", map[string]string{"Accept": "application/x-yaml"})
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, `"code":"not_found"`) {
		t.Errorf("body %q", body)
	}
}

func TestConsumesDecodesByContentType(t *testing.T) {
	srv := gameServer(t)
	for _, tc := range []struct {
		name, contentType, body string
	}{
		{"yaml", "application/x-yaml", questYAML},
		{"json", "application/json; charset=utf-8", questJSON},
		{"no content type: first of Consumes", "", questYAML},
	} {
		h := map[string]string{}
		if tc.contentType != "" {
			h["Content-Type"] = tc.contentType
		}
		resp, body := do(t, "PUT", srv.URL+"/test/games", tc.body, h)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, body %q", tc.name, resp.StatusCode, body)
			continue
		}
		if body != questJSON {
			t.Errorf("%s: echoed %q, want %q", tc.name, body, questJSON)
		}
	}
}

func TestConsumesUnlistedIs415(t *testing.T) {
	srv := gameServer(t)
	resp, body := do(t, "PUT", srv.URL+"/test/games", "<game/>", map[string]string{"Content-Type": "application/xml"})
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d, want 415", resp.StatusCode)
	}
	if !strings.Contains(body, `"code":"unsupported_media_type"`) {
		t.Errorf("body %q", body)
	}
}

func TestConsumesInvalidYAMLIs400(t *testing.T) {
	srv := gameServer(t)
	resp, body := do(t, "PUT", srv.URL+"/test/games", "title: [", map[string]string{"Content-Type": "application/x-yaml"})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"code":"bad_request"`) {
		t.Fatalf("status %d, body %q", resp.StatusCode, body)
	}
}

func TestDefaultContentTypeIsJSONOnly(t *testing.T) {
	srv := serve(t, NewRoute[struct{}, langResp](langEndpoint{}, "", ""))
	resp, _ := do(t, "GET", srv.URL+"/test/lang", "", map[string]string{"Accept": "application/x-yaml"})
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("status %d, want 406", resp.StatusCode)
	}
}

type badProducesEndpoint struct{ langEndpoint }

func (badProducesEndpoint) Produces() []string { return []string{"text/csv"} }

func TestNewRouteRejectsUnsupportedMediaType(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), "text/csv") {
			t.Fatalf("recover() = %v, want a panic naming text/csv", r)
		}
	}()
	NewRoute[struct{}, langResp](badProducesEndpoint{}, "", "")
}

func TestYAMLKeepsStringsThatLookLikeOtherTypes(t *testing.T) {
	in := game{Title: "true", Tags: []string{"4", "null", "a: b"}}
	var buf strings.Builder
	if err := (yamlCodec{}).encode(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out game
	if err := (yamlCodec{}).decode([]byte(buf.String()), &out); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	if out.Title != "true" || strings.Join(out.Tags, "|") != "4|null|a: b" {
		t.Fatalf("round trip of %q = %+v", buf.String(), out)
	}
}

// --- Server-sent events ---

type tickReq struct {
	N    int  `query:"n"`
	Fail bool `query:"fail"`
	Nil  bool `query:"nil"`
	Hold bool `query:"hold"` // send nothing, wait for ctx
}

type tick struct {
	Seq  int    `json:"seq"`
	Lang string `json:"lang"`
}

// tickEndpoint sends N ticks, then closes; it reports when its ctx ends.
type tickEndpoint struct{ done chan struct{} }

func (tickEndpoint) Method() string  { return "GET" }
func (tickEndpoint) Path() string    { return "/test/ticks" }
func (tickEndpoint) Auth() AuthLevel { return AuthNone }
func (e tickEndpoint) Stream(ctx context.Context, req tickReq) (<-chan tick, error) {
	if req.Fail {
		return nil, Error(ErrConflict, "busy", "workshop is busy")
	}
	if req.Nil {
		return nil, nil
	}
	lang := Header(ctx, "Accept-Language")
	ch := make(chan tick)
	go func() {
		defer close(ch)
		if req.Hold {
			<-ctx.Done()
			if e.done != nil {
				close(e.done)
			}
			return
		}
		for i := 1; i <= req.N; i++ {
			select {
			case ch <- tick{Seq: i, Lang: lang}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func TestStreamWritesEventsUntilChannelCloses(t *testing.T) {
	srv := serve(t, NewStreamRoute[tickReq, tick](tickEndpoint{}, "", ""))
	resp, body := do(t, "GET", srv.URL+"/test/ticks?n=2", "", map[string]string{"Accept-Language": "fr"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	want := "data: {\"seq\":1,\"lang\":\"fr\"}\n\ndata: {\"seq\":2,\"lang\":\"fr\"}\n\n"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestStreamErrorAnswersLikeHandle(t *testing.T) {
	srv := serve(t, NewStreamRoute[tickReq, tick](tickEndpoint{}, "", ""))
	resp, body := do(t, "GET", srv.URL+"/test/ticks?fail=true", "", nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, `"code":"busy"`) {
		t.Fatalf("status %d, body %q", resp.StatusCode, body)
	}
	resp, body = do(t, "GET", srv.URL+"/test/ticks?nil=true", "", nil)
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(body, `"code":"internal"`) {
		t.Fatalf("nil channel: status %d, body %q", resp.StatusCode, body)
	}
}

func TestStreamCancelsCtxWhenClientLeaves(t *testing.T) {
	done := make(chan struct{})
	srv := serve(t, NewStreamRoute[tickReq, tick](tickEndpoint{done: done}, "", ""))
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/test/ticks?hold=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	// Headers arrive before any event.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cancel()
	resp.Body.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stream's ctx was not cancelled after the client left")
	}
}

func TestStreamSendsKeepAlive(t *testing.T) {
	old := streamKeepAlive
	streamKeepAlive = 20 * time.Millisecond
	t.Cleanup(func() { streamKeepAlive = old })
	srv := serve(t, NewStreamRoute[tickReq, tick](tickEndpoint{}, "", ""))
	resp, err := http.Get(srv.URL + "/test/ticks?hold=true")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, len(": keepalive\n\n"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != ": keepalive\n\n" {
		t.Fatalf("got %q", buf)
	}
}

// lateSessionEndpoint calls StartSessionCtx from the sending goroutine, after
// Stream returned and headers went out.
type lateSessionEndpoint struct{ err chan error }

func (lateSessionEndpoint) Method() string  { return "GET" }
func (lateSessionEndpoint) Path() string    { return "/test/late" }
func (lateSessionEndpoint) Auth() AuthLevel { return AuthNone }
func (e lateSessionEndpoint) Stream(ctx context.Context, _ struct{}) (<-chan tick, error) {
	ch := make(chan tick)
	go func() {
		defer close(ch)
		ch <- tick{Seq: 1} // received only after headers are flushed
		_, err := StartSessionCtx(ctx, "late")
		e.err <- err
	}()
	return ch, nil
}

func TestStartSessionCtxAfterStreamStartedFails(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	ep := lateSessionEndpoint{err: make(chan error, 1)}
	Register(NewStreamRoute[struct{}, tick](ep, "", ""))
	e := newAuthEnv(t)
	resp := e.do("GET", "/test/late", reqOpts{})
	io.ReadAll(resp.Body)
	err := <-ep.err
	if err == nil || !strings.Contains(err.Error(), "before Stream returns") {
		t.Fatalf("err = %v, want an error saying sessions start before Stream returns", err)
	}
	if len(e.store.sessions) != 0 {
		t.Errorf("a session was created: %v", e.store.sessions)
	}
}

func TestNegotiateEdgeCases(t *testing.T) {
	produces := []string{"application/x-yaml", "application/json"}
	for _, tc := range []struct {
		accept, want string
		ok           bool
	}{
		{"*", "application/x-yaml", true},                                                // legacy bare wildcard
		{"application/json;q=abc, application/x-yaml;q=0.1", "application/x-yaml", true}, // bad q: range ignored
		{";;;", "application/x-yaml", true},                                              // nothing parsable: as if absent
		{"application/json;q=0, application/x-yaml;q=0", "", false},
	} {
		got, ok := negotiate(tc.accept, produces)
		if got != tc.want || ok != tc.ok {
			t.Errorf("negotiate(%q) = %q, %v; want %q, %v", tc.accept, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVaryAcceptWhenSeveralFormats(t *testing.T) {
	srv := gameServer(t)
	resp, _ := do(t, "GET", srv.URL+"/test/games/7", "", nil)
	if got := resp.Header.Get("Vary"); got != "Accept" {
		t.Errorf("Vary = %q, want Accept", got)
	}
	srv = serve(t, NewRoute[struct{}, langResp](langEndpoint{}, "", ""))
	resp, _ = do(t, "GET", srv.URL+"/test/lang", "", nil)
	if got := resp.Header.Get("Vary"); got != "" {
		t.Errorf("single format: Vary = %q, want none", got)
	}
}
