package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flocko-motion/schemaf/api"
)

const scanHeader = `package api

import (
	"context"
	"net/http"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

var _ http.Handler
var _ context.Context

type Req struct{}
type Resp struct{}
`

func scanSource(t *testing.T, src string) ([]endpointInfo, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ep.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	eps, _, err := scanAPIDir(dir)
	return eps, err
}

func TestScanAuthLevels(t *testing.T) {
	eps, err := scanSource(t, scanHeader+`
type NoneEndpoint struct{}
func (e NoneEndpoint) Method() string { return "GET" }
func (e NoneEndpoint) Path() string { return "/api/none" }
func (e NoneEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e NoneEndpoint) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }

type OptionalEndpoint struct{}
func (e OptionalEndpoint) Method() string { return "GET" }
func (e OptionalEndpoint) Path() string { return `+"`/api/optional`"+` }
func (e OptionalEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthOptional }
func (e OptionalEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error { return nil }

type RequiredEndpoint struct{}
func (e RequiredEndpoint) Method() string { return "POST" }
func (e RequiredEndpoint) Path() string { return "/api/required" }
func (e RequiredEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthRequired }
func (e RequiredEndpoint) Handle(ctx context.Context, req *Req) ([]Resp, error) { return nil, nil }

// Not an endpoint: no Handle/HandleRaw.
type Helper struct{}
func (h Helper) Path() string { return "x" }
`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]endpointInfo{}
	for _, ep := range eps {
		got[ep.StructName] = ep
	}
	if len(got) != 3 {
		t.Fatalf("got %d endpoints: %v", len(got), eps)
	}
	if g := got["NoneEndpoint"]; g.Auth != api.AuthNone || g.ReqType != "Req" || g.RespType != "Resp" {
		t.Errorf("NoneEndpoint = %+v", g)
	}
	if g := got["OptionalEndpoint"]; g.Auth != api.AuthOptional || !g.IsRaw || g.Path != "/api/optional" {
		t.Errorf("OptionalEndpoint = %+v", g)
	}
	if g := got["RequiredEndpoint"]; g.Auth != api.AuthRequired || g.ReqType != "*Req" || g.RespType != "[]Resp" {
		t.Errorf("RequiredEndpoint = %+v", g)
	}
}

func TestScanDefaultImportName(t *testing.T) {
	eps, err := scanSource(t, `package api

import (
	"context"

	"github.com/flocko-motion/schemaf/api"
)

type Req struct{}
type Resp struct{}

type E struct{}
func (e E) Method() string { return "GET" }
func (e E) Path() string { return "/api/e" }
func (e E) Auth() api.AuthLevel { return api.AuthOptional }
func (e E) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`)
	if err != nil || len(eps) != 1 || eps[0].Auth != api.AuthOptional {
		t.Fatalf("eps %+v, err %v", eps, err)
	}
}

func TestScanErrors(t *testing.T) {
	const okMethod = `func (e E) Method() string { return "GET" }
`
	const okPath = `func (e E) Path() string { return "/api/e" }
`
	const okAuth = `func (e E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
`
	const okHandle = `func (e E) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`
	cases := []struct {
		name, body, want string
	}{
		{"no handler (misnamed Handle)", okMethod + okPath + okAuth +
			`func (e E) Handler(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }`,
			"endpoint E has Method(), Path() and Auth() but no Handle or HandleRaw method"},
		{"old Auth() bool", okMethod + okPath + okHandle +
			`func (e E) Auth() bool { return true }`,
			"E.Auth() returns bool; since schemaf v1.11.0 it returns api.AuthLevel: true → api.AuthRequired, false → api.AuthNone"},
		{"Auth() not a constant", okMethod + okPath + okHandle +
			`func (e E) Auth() schemafapi.AuthLevel { return schemafapi.AuthLevel(2) }`,
			"endpoint E: Auth() must return one of api.AuthNone, api.AuthOptional, api.AuthRequired"},
		{"Auth() unknown constant", okMethod + okPath + okHandle +
			`func (e E) Auth() schemafapi.AuthLevel { return schemafapi.AuthMaybe }`,
			"endpoint E: Auth() must return one of"},
		{"Auth() with logic", okMethod + okPath + okHandle +
			`func (e E) Auth() schemafapi.AuthLevel { if true { return schemafapi.AuthNone }; return schemafapi.AuthRequired }`,
			"endpoint E: Auth() must return one of"},
		{"Auth() wrong type", okMethod + okPath + okHandle +
			`func (e E) Auth() int { return 0 }`,
			"endpoint E: Auth() must return api.AuthLevel"},
		{"Method() not a literal", okPath + okAuth + okHandle +
			`func (e E) Method() string { return http.MethodGet }`,
			"endpoint E: Method() must return a string literal"},
		{"Path() not a literal", okMethod + okAuth + okHandle +
			`const p = "/api/e"
func (e E) Path() string { return p }`,
			"endpoint E: Path() must return a string literal"},
		{"Handle without ctx", okMethod + okPath + okAuth +
			`func (e E) Handle(req Req) (Resp, error) { return Resp{}, nil }`,
			"endpoint E: Handle must have the signature"},
		{"Handle without error", okMethod + okPath + okAuth +
			`func (e E) Handle(ctx context.Context, req Req) Resp { return Resp{} }`,
			"endpoint E: Handle must have the signature"},
		{"HandleRaw wrong shape", okMethod + okPath + okAuth +
			`func (e E) HandleRaw(r *http.Request) error { return nil }`,
			"endpoint E: HandleRaw must have the signature"},
		{"missing Auth", okMethod + okPath + okHandle,
			"endpoint E is missing Auth()"},
		{"both handlers", okMethod + okPath + okAuth + okHandle +
			`func (e E) HandleRaw(w http.ResponseWriter, r *http.Request) error { return nil }`,
			"has both Handle and HandleRaw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eps, err := scanSource(t, scanHeader+"\ntype E struct{}\n"+tc.body+"\n")
			if err == nil {
				t.Fatalf("no error (endpoints %+v)", eps)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q\nwant it to contain %q", err, tc.want)
			}
		})
	}
}

// TestScanReportsAllErrors: every broken endpoint is reported, not just the first.
func TestScanReportsAllErrors(t *testing.T) {
	_, err := scanSource(t, scanHeader+`
type A struct{}
func (e A) Method() string { return "GET" }
func (e A) Path() string { return "/a" }
func (e A) Auth() bool { return false }
func (e A) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }

type B struct{}
func (e B) Method() string { return "GET" }
func (e B) Path() string { return "/b" }
func (e B) Auth() bool { return true }
func (e B) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`)
	if err == nil || !strings.Contains(err.Error(), "A.Auth() returns bool") || !strings.Contains(err.Error(), "B.Auth() returns bool") {
		t.Fatalf("err = %v", err)
	}
}
