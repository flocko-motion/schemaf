package codegen

import (
	"encoding/json"
	"strings"
	"testing"
)

// paramsEndpoint renders an endpoint struct name with method, path and a
// Handle taking req and returning Resp.
func paramsEndpoint(name, method, path, req string) string {
	return `
type ` + name + ` struct{}

func (` + name + `) Method() string             { return "` + method + `" }
func (` + name + `) Path() string               { return "` + path + `" }
func (` + name + `) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (` + name + `) Handle(ctx context.Context, req ` + req + `) (Resp, error) { return Resp{}, nil }
`
}

const paramsHeader = `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

type Resp struct {
	OK bool ` + "`json:\"ok\"`" + `
}

type Role string
`

// TestOpenAPIPathAndQueryParams: typed path params, required scalar,
// optional pointer and repeated slice query params; path and query fields
// stay out of the body, and a request of only params has no requestBody.
func TestOpenAPIPathAndQueryParams(t *testing.T) {
	src := paramsHeader + `
type ListGamesReq struct {
	PlayerID int      ` + "`path:\"playerId\"`" + `
	Sort     string   ` + "`query:\"sort\"`" + `
	Search   *string  ` + "`query:\"search\"`" + `
	Tags     []string ` + "`query:\"tag\"`" + `
	Mine     *bool    ` + "`query:\"mine\"`" + `
	Roles    []Role   ` + "`query:\"role\"`" + `
	IDs      []uint32 ` + "`query:\"id\"`" + `
}

type UpdateGameReq struct {
	ID     int64  ` + "`path:\"id\"`" + `
	DryRun *bool  ` + "`query:\"dryRun\"`" + `
	Title  string ` + "`json:\"title\"`" + `
}

type DeleteGameReq struct {
	ID  bool ` + "`path:\"id\"`" + `
	Why Role ` + "`query:\"why\"`" + `
}

type TouchGameReq struct {
	ID string ` + "`path:\"id\"`" + `
}
` + paramsEndpoint("ListGamesEndpoint", "GET", "/api/players/{playerId}/games", "ListGamesReq") +
		paramsEndpoint("UpdateGameEndpoint", "PUT", "/api/games/{id}", "UpdateGameReq") +
		paramsEndpoint("DeleteGameEndpoint", "DELETE", "/api/games/{id}", "DeleteGameReq") +
		paramsEndpoint("TouchGameEndpoint", "POST", "/api/games/{id}/touch", "TouchGameReq")

	eps, schemas, _, err := schemaProject(t, map[string]string{"api/games.go": src})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := buildOpenAPISpec("myapp", eps, schemas)
	if err != nil {
		t.Fatal(err)
	}
	var spec staticSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	params := func(path, method string) string { return jsonOf(t, spec.Paths[path][method]["parameters"]) }

	want := `[` +
		`{"in":"path","name":"playerId","required":true,"schema":{"type":"integer"}},` +
		`{"in":"query","name":"sort","required":true,"schema":{"type":"string"}},` +
		`{"in":"query","name":"search","required":false,"schema":{"type":"string"}},` +
		`{"explode":true,"in":"query","name":"tag","required":false,"schema":{"items":{"type":"string"},"type":"array"},"style":"form"},` +
		`{"in":"query","name":"mine","required":false,"schema":{"type":"boolean"}},` +
		`{"explode":true,"in":"query","name":"role","required":false,"schema":{"items":{"type":"string"},"type":"array"},"style":"form"},` +
		`{"explode":true,"in":"query","name":"id","required":false,"schema":{"items":{"type":"integer"},"type":"array"},"style":"form"}]`
	if got := params("/api/players/{playerId}/games", "get"); got != want {
		t.Errorf("list params =\n %s\nwant\n %s", got, want)
	}
	if got := params("/api/games/{id}", "put"); got != `[{"in":"path","name":"id","required":true,"schema":{"type":"integer"}},{"in":"query","name":"dryRun","required":false,"schema":{"type":"boolean"}}]` {
		t.Errorf("update params = %s", got)
	}
	if got := params("/api/games/{id}", "delete"); got != `[{"in":"path","name":"id","required":true,"schema":{"type":"boolean"}},{"in":"query","name":"why","required":true,"schema":{"type":"string"}}]` {
		t.Errorf("delete params = %s", got)
	}

	// The body schema of the PUT has only the JSON field.
	if got := jsonOf(t, schemas["UpdateGameReq"]); got != `{"properties":{"title":{"type":"string"}},"required":["title"],"type":"object"}` {
		t.Errorf("UpdateGameReq schema = %s", got)
	}
	// A POST whose request has only path fields has no body.
	if _, ok := spec.Paths["/api/games/{id}/touch"]["post"]["requestBody"]; ok {
		t.Error("POST with only path fields has a requestBody")
	}
	if _, ok := schemas["TouchGameReq"]; ok {
		t.Error("TouchGameReq got a component schema")
	}
}

// TestOpenAPIParamErrors: each misuse of path and query fields is a codegen
// error naming the endpoint and the field.
func TestOpenAPIParamErrors(t *testing.T) {
	src := paramsHeader + `
type FloatReq struct {
	N float64 ` + "`query:\"n\"`" + `
}
type StructReq struct {
	R Resp ` + "`query:\"r\"`" + `
}
type MapReq struct {
	M map[string]string ` + "`query:\"m\"`" + `
}
type PtrSliceReq struct {
	S *[]string ` + "`query:\"s\"`" + `
}
type PathPtrReq struct {
	ID *int ` + "`path:\"id\"`" + `
}
type PathSliceReq struct {
	ID []int ` + "`path:\"id\"`" + `
}
type MissingPathReq struct{}
type ExtraPathReq struct {
	ID string ` + "`path:\"id\"`" + `
}
type JSONQueryReq struct {
	Title string ` + "`json:\"title\"`" + `
	Q     string ` + "`json:\"q\" query:\"q\"`" + `
}
type JSONPathReq struct {
	ID string ` + "`json:\"id\" path:\"id\"`" + `
}
type UntaggedGetReq struct {
	Search string
}
type UntaggedDeleteReq struct {
	Reason string ` + "`json:\"reason\"`" + `
}
type DupPathReq struct {
	A string ` + "`path:\"id\"`" + `
	B string ` + "`path:\"id\"`" + `
}
type UnexportedReq struct {
	q string ` + "`query:\"q\"`" + `
}
type Paging struct {
	Page *int ` + "`query:\"page\"`" + `
}
type GetGameReq struct {
	ID string ` + "`path:\"id\"`" + `
}
type WrapsReqResp struct {
	Req GetGameReq ` + "`json:\"req\"`" + `
}
type GetGameEndpoint struct{}

func (GetGameEndpoint) Method() string             { return "GET" }
func (GetGameEndpoint) Path() string               { return "/api/games/{id}" }
func (GetGameEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (GetGameEndpoint) Handle(ctx context.Context, req GetGameReq) (*GetGameReq, error) { return nil, nil }

type NoReq struct{}
type WrapEndpoint struct{}

func (WrapEndpoint) Method() string             { return "GET" }
func (WrapEndpoint) Path() string               { return "/api/wrap" }
func (WrapEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (WrapEndpoint) Handle(ctx context.Context, req NoReq) (WrapsReqResp, error) { return WrapsReqResp{}, nil }

` + paramsEndpoint("FloatEndpoint", "GET", "/api/float", "FloatReq") +
		paramsEndpoint("StructEndpoint", "GET", "/api/struct", "StructReq") +
		paramsEndpoint("MapEndpoint", "GET", "/api/map", "MapReq") +
		paramsEndpoint("PtrSliceEndpoint", "GET", "/api/ptrslice", "PtrSliceReq") +
		paramsEndpoint("PathPtrEndpoint", "GET", "/api/pathptr/{id}", "PathPtrReq") +
		paramsEndpoint("PathSliceEndpoint", "GET", "/api/pathslice/{id}", "PathSliceReq") +
		paramsEndpoint("MissingPathEndpoint", "GET", "/api/missing/{id}", "MissingPathReq") +
		paramsEndpoint("ExtraPathEndpoint", "GET", "/api/extra", "ExtraPathReq") +
		paramsEndpoint("JSONQueryEndpoint", "POST", "/api/jsonquery", "JSONQueryReq") +
		paramsEndpoint("JSONPathEndpoint", "PUT", "/api/jsonpath/{id}", "JSONPathReq") +
		paramsEndpoint("UntaggedGetEndpoint", "GET", "/api/untaggedget", "UntaggedGetReq") +
		paramsEndpoint("UntaggedDeleteEndpoint", "DELETE", "/api/untaggeddelete", "UntaggedDeleteReq") +
		paramsEndpoint("DupPathEndpoint", "GET", "/api/dup/{id}", "DupPathReq") +
		paramsEndpoint("UnexportedEndpoint", "GET", "/api/unexported", "UnexportedReq")

	_, _, _, err := schemaProject(t, map[string]string{"api/bad.go": src})
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	for _, want := range []string{
		"endpoint FloatEndpoint: request type FloatReq, field N: query field of type float64",
		"endpoint StructEndpoint: request type StructReq, field R: query field of type",
		"endpoint MapEndpoint: request type MapReq, field M: query field of type map[string]string",
		"endpoint PtrSliceEndpoint: request type PtrSliceReq, field S: query field of type *[]string",
		"endpoint PathPtrEndpoint: request type PathPtrReq, field ID: path field of type *int",
		"endpoint PathSliceEndpoint: request type PathSliceReq, field ID: path field of type []int",
		"endpoint MissingPathEndpoint: request type MissingPathReq: Path() \"/api/missing/{id}\" has {id}, but no field is tagged `path:\"id\"`",
		"endpoint ExtraPathEndpoint: request type ExtraPathReq: a field is tagged `path:\"id\"`, but Path() \"/api/extra\" has no {id}",
		"endpoint JSONQueryEndpoint: request type JSONQueryReq, field Q: tagged both `json` and `query`",
		"endpoint JSONPathEndpoint: request type JSONPathReq, field ID: tagged both `json` and `path`",
		"endpoint UntaggedGetEndpoint: request type UntaggedGetReq, field Search: a GET request has no body",
		"endpoint UntaggedDeleteEndpoint: request type UntaggedDeleteReq, field Reason: a DELETE request has no body",
		"endpoint DupPathEndpoint: request type DupPathReq, field B: field A is already tagged `path:\"id\"`",
		"endpoint UnexportedEndpoint: request type UnexportedReq, field q: the field is unexported",
		"GetGameEndpoint response: type GetGameReq has path or query fields, so it is a request type and can't be used here",
		"WrapsReqResp.Req: type GetGameReq has path or query fields, so it is a request type and can't be used here",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q", want)
		}
	}
	for _, once := range []string{"GetGameEndpoint response: type GetGameReq", "WrapsReqResp.Req: type GetGameReq"} {
		if n := strings.Count(msg, once); n != 1 {
			t.Errorf("%q reported %d times, want once", once, n)
		}
	}
	if t.Failed() {
		t.Logf("error:\n%s", msg)
	}
}
