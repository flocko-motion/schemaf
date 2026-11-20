package codegen

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// offlineGoMod returns go.mod and go.sum content for a module that requires
// schemaf at schemafVersion, served from this checkout, plus each module in
// deps (path → directory, relative to the go.mod), and makes the go tool run
// offline for the rest of the test (schemaf's modules come from the module
// cache, filled by requireModuleCache).
func offlineGoMod(t *testing.T, module, schemafVersion string, deps map[string]string) (gomod, gosum string) {
	t.Helper()
	requireModuleCache(t)
	checkout, err := filepath.Abs(schemafCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	gomod = "module " + module + "\n\ngo 1.25.0\n\nrequire " + schemafModule + " " + schemafVersion + "\n\nreplace " + schemafModule + " => " + checkout + "\n"
	for _, path := range slices.Sorted(maps.Keys(deps)) {
		gomod += "\nrequire " + path + " v0.0.0\n\nreplace " + path + " => " + deps[path] + "\n"
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	return gomod, readFile(t, filepath.Join(checkout, "go.sum"))
}

// schemaProject writes a project (go/go.mod, module example.com/app) with
// the given files (paths relative to go/) into a temp dir, chdirs to its
// root, scans go/api and builds the schemas. github.com/google/uuid and
// example.com/dep are local stand-ins in go/fakeuuid and go/fakedep.
func schemaProject(t *testing.T, files map[string]string) ([]endpointInfo, map[string]any, []string, error) {
	t.Helper()
	gomod, gosum := offlineGoMod(t, "example.com/app", "v0.0.0", map[string]string{
		"github.com/google/uuid": "./fakeuuid",
		"example.com/dep":        "./fakedep",
	})
	all := map[string]string{
		"go.mod":           gomod,
		"go.sum":           gosum,
		"fakeuuid/go.mod":  "module github.com/google/uuid\n\ngo 1.25.0\n",
		"fakeuuid/uuid.go": "package uuid\n\ntype UUID [16]byte\n",
		"fakedep/go.mod":   "module example.com/dep\n\ngo 1.25.0\n",
		"fakedep/money.go": "package dep\n\ntype Money struct {\n\tCents int64 `json:\"cents\"`\n}\n",
	}
	for p, c := range files {
		all[p] = c
	}
	root := t.TempDir()
	for p, c := range all {
		full := filepath.Join(root, "go", p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	eps, err := scanAPIDir(apiDir)
	if err != nil {
		t.Fatal(err)
	}
	schemas, sources, err := loadSchemas(apiDir, eps)
	return eps, schemas, sources, err
}

const objUser = `package obj

import (
	"time"

	"github.com/google/uuid"
)

type Base struct {
	ID        uuid.UUID ` + "`json:\"id\"`" + `
	CreatedAt time.Time ` + "`json:\"createdAt\"`" + `
}

type Role string

type User struct {
	Base
	Name     string            ` + "`json:\"name\"`" + `
	Email    *string           ` + "`json:\"email\"`" + `
	Nick     string            ` + "`json:\"nick,omitempty\"`" + `
	Roles    []Role            ` + "`json:\"roles\"`" + `
	Address  Address           ` + "`json:\"address\"`" + `
	Labels   map[string]string ` + "`json:\"labels\"`" + `
	Secret   string            ` + "`json:\"-\"`" + `
	Untagged int
	hidden   int
}

type Address struct {
	City string ` + "`json:\"city\"`" + `
}
`

const apiUsers = `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type GetUserReq struct {
	ID string ` + "`path:\"id\"`" + `
}

type UserResp = obj.User

type GetUserEndpoint struct{}

func (e GetUserEndpoint) Method() string               { return "GET" }
func (e GetUserEndpoint) Path() string                 { return "/api/users/{id}" }
func (e GetUserEndpoint) Auth() schemafapi.AuthLevel   { return schemafapi.AuthNone }
func (e GetUserEndpoint) Handle(ctx context.Context, req GetUserReq) (UserResp, error) {
	return UserResp{}, nil
}

type ListUsersReq struct{}

type ListUsersResp struct {
	Users []obj.User ` + "`json:\"users\"`" + `
}

type ListUsersEndpoint struct{}

func (e ListUsersEndpoint) Method() string             { return "GET" }
func (e ListUsersEndpoint) Path() string               { return "/api/users" }
func (e ListUsersEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e ListUsersEndpoint) Handle(ctx context.Context, req ListUsersReq) (ListUsersResp, error) {
	return ListUsersResp{}, nil
}
`

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSchemasFromOtherPackageAndAlias: an alias in go/api and a field of a
// type from another package both produce the full User schema.
func TestSchemasFromOtherPackageAndAlias(t *testing.T) {
	eps, schemas, sources, err := schemaProject(t, map[string]string{
		"obj/user.go":  objUser,
		"api/users.go": apiUsers,
		// A stale generated file must not break type checking.
		"api/endpoints.gen.go": "package api\n\nfunc Provider() { undefinedThing() }\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]endpointInfo{}
	for _, ep := range eps {
		byName[ep.StructName] = ep
	}
	if got := jsonOf(t, byName["GetUserEndpoint"].RespSchema); got != `{"$ref":"#/components/schemas/User"}` {
		t.Errorf("GetUser response schema = %s", got)
	}
	if got := jsonOf(t, byName["ListUsersEndpoint"].RespSchema); got != `{"$ref":"#/components/schemas/ListUsersResp"}` {
		t.Errorf("ListUsers response schema = %s", got)
	}
	if got := jsonOf(t, schemas["ListUsersResp"]); got != `{"properties":{"users":{"items":{"$ref":"#/components/schemas/User"},"type":"array"}},"required":["users"],"type":"object"}` {
		t.Errorf("ListUsersResp = %s", got)
	}
	want := `{"properties":{` +
		`"Untagged":{"type":"integer"},` +
		`"address":{"$ref":"#/components/schemas/Address"},` +
		`"createdAt":{"format":"date-time","type":"string"},` +
		`"email":{"type":"string"},` +
		`"id":{"format":"uuid","type":"string"},` +
		`"labels":{"additionalProperties":{"type":"string"},"type":"object"},` +
		`"name":{"type":"string"},` +
		`"nick":{"type":"string"},` +
		`"roles":{"items":{"type":"string"},"type":"array"}},` +
		`"required":["id","createdAt","name","roles","address","labels","Untagged"],"type":"object"}`
	if got := jsonOf(t, schemas["User"]); got != want {
		t.Errorf("User =\n %s\nwant\n %s", got, want)
	}
	if got := jsonOf(t, schemas["Address"]); got != `{"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"}` {
		t.Errorf("Address = %s", got)
	}
	if got := strings.Join(sources, ","); got != "go/obj/*.go" {
		t.Errorf("sources = %q, want go/obj/*.go (uuid and time are built in, api is an input already)", got)
	}
	for _, name := range []string{"UserResp", "Base", "Role"} {
		if _, ok := schemas[name]; ok {
			t.Errorf("unexpected schema %s", name)
		}
	}
}

// TestSchemaNameCollision: two types with the same name from different
// packages are a codegen error naming both.
func TestSchemaNameCollision(t *testing.T) {
	_, _, _, err := schemaProject(t, map[string]string{
		"obj/user.go": objUser,
		"api/users.go": apiUsers + `
type Address struct {
	Street string ` + "`json:\"street\"`" + `
}

type GetAddrEndpoint struct{}

func (e GetAddrEndpoint) Method() string             { return "GET" }
func (e GetAddrEndpoint) Path() string               { return "/api/addr" }
func (e GetAddrEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e GetAddrEndpoint) Handle(ctx context.Context, req ListUsersReq) (Address, error) {
	return Address{}, nil
}
`,
	})
	if err == nil || !strings.Contains(err.Error(), "example.com/app/api.Address") || !strings.Contains(err.Error(), "example.com/app/obj.Address") {
		t.Fatalf("err = %v, want a collision error naming both types", err)
	}
}

// TestSchemaUnsupportedType: a type without a JSON shape is a codegen error.
func TestSchemaUnsupportedType(t *testing.T) {
	_, _, _, err := schemaProject(t, map[string]string{
		"obj/user.go": objUser,
		"api/users.go": apiUsers + `
type ChanResp struct {
	C chan int ` + "`json:\"c\"`" + `
}

type ChanEndpoint struct{}

func (e ChanEndpoint) Method() string             { return "GET" }
func (e ChanEndpoint) Path() string               { return "/api/chan" }
func (e ChanEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e ChanEndpoint) Handle(ctx context.Context, req ListUsersReq) (ChanResp, error) {
	return ChanResp{}, nil
}
`,
	})
	if err == nil || !strings.Contains(err.Error(), "ChanResp.C") || !strings.Contains(err.Error(), "chan int") {
		t.Fatalf("err = %v, want an error naming ChanResp.C and its type", err)
	}
}

// TestSchemaTypeErrorFails: a package that does not type-check is a codegen
// error, never a half-built spec.
func TestSchemaTypeErrorFails(t *testing.T) {
	_, _, _, err := schemaProject(t, map[string]string{
		"obj/user.go":  objUser,
		"api/users.go": apiUsers + "\nvar _ = nosuchname\n",
	})
	if err == nil || !strings.Contains(err.Error(), "nosuchname") {
		t.Fatalf("err = %v, want the type error", err)
	}
}

const apiCreate = `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type Level int

func (l Level) MarshalText() ([]byte, error) { return nil, nil }

type CreateUserReq struct {
	User  obj.User ` + "`json:\"user\"`" + `
	Level Level    ` + "`json:\"level\"`" + `
	Count int64    ` + "`json:\"count,string\"`" + `
}

type CreateUserEndpoint struct{}

func (e CreateUserEndpoint) Method() string             { return "POST" }
func (e CreateUserEndpoint) Path() string               { return "/api/users" }
func (e CreateUserEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthRequired }
func (e CreateUserEndpoint) Handle(ctx context.Context, req CreateUserReq) ([]obj.User, error) {
	return nil, nil
}
`

// TestSchemaRequestBodyAndSliceResponse: a POST request body and a slice
// response, end to end through openapi.json.
func TestSchemaRequestBodyAndSliceResponse(t *testing.T) {
	eps, schemas, _, err := schemaProject(t, map[string]string{"obj/user.go": objUser, "api/create.go": apiCreate})
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
	op := spec.Paths["/api/users"]["post"]
	if got := jsonOf(t, op["requestBody"]); got != `{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/CreateUserReq"}}},"required":true}` {
		t.Errorf("requestBody = %s", got)
	}
	if got := jsonOf(t, op["responses"].(map[string]any)["200"]); got != `{"content":{"application/json":{"schema":{"items":{"$ref":"#/components/schemas/User"},"type":"array"}}},"description":"OK"}` {
		t.Errorf("200 = %s", got)
	}
	if got := jsonOf(t, spec.Components.Schemas["CreateUserReq"]); got != `{"properties":{"count":{"type":"string"},"level":{"type":"string"},"user":{"$ref":"#/components/schemas/User"}},"required":["user","level","count"],"type":"object"}` {
		t.Errorf("CreateUserReq = %s", got)
	}
	if _, ok := spec.Components.Schemas["User"]; !ok {
		t.Error("User schema missing")
	}
}

// TestSchemaJSONMarshalerFails: a type with its own MarshalJSON has an
// unknown JSON shape; codegen names it instead of guessing.
func TestSchemaJSONMarshalerFails(t *testing.T) {
	_, _, _, err := schemaProject(t, map[string]string{
		"obj/user.go":   objUser,
		"api/create.go": strings.Replace(apiCreate, "MarshalText() ([]byte, error)", "MarshalJSON() ([]byte, error)", 1),
	})
	if err == nil || !strings.Contains(err.Error(), "example.com/app/api.Level") || !strings.Contains(err.Error(), "CreateUserReq.Level") {
		t.Fatalf("err = %v, want an error naming Level and where it is used", err)
	}
}

// apiEndpoint returns an api package file declaring endpoint E with the
// given method, Req and Resp types, plus extra declarations.
func apiEndpoint(method, req, resp, extra string) string {
	return `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
	"example.com/dep"
)

var _ obj.Address
var _ dep.Money

type Empty struct{}

type E struct{}

func (e E) Method() string             { return "` + method + `" }
func (e E) Path() string               { return "/api/e" }
func (e E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e E) Handle(ctx context.Context, req ` + req + `) (` + resp + `, error) {
	panic("unused")
}
` + extra
}

// TestSchemaEmbeddedPointerAndStringPointer: fields promoted through an
// embedded pointer are optional (encoding/json leaves them all out when it
// is nil), and `json:",string"` applies through a pointer.
func TestSchemaEmbeddedPointerAndStringPointer(t *testing.T) {
	_, schemas, _, err := schemaProject(t, map[string]string{
		"obj/user.go": objUser,
		"api/e.go": apiEndpoint("GET", "Empty", "Resp", `
type Resp struct {
	*obj.Base
	N *int64 `+"`json:\"n,string\"`"+`
}
`),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"properties":{"createdAt":{"format":"date-time","type":"string"},"id":{"format":"uuid","type":"string"},"n":{"type":"string"}},"type":"object"}`
	if got := jsonOf(t, schemas["Resp"]); got != want {
		t.Errorf("Resp =\n %s\nwant\n %s", got, want)
	}
}

// TestSchemaNonStructRequestBody: a POST request type that isn't a struct is
// a codegen error, never a silently dropped body.
func TestSchemaNonStructRequestBody(t *testing.T) {
	_, _, _, err := schemaProject(t, map[string]string{
		"obj/user.go": objUser,
		"api/e.go":    apiEndpoint("POST", "[]obj.Address", "Empty", ""),
	})
	if err == nil || !strings.Contains(err.Error(), "endpoint E") || !strings.Contains(err.Error(), "not a struct") {
		t.Fatalf("err = %v, want an error naming E and the non-struct request", err)
	}
}

// TestSchemaSources: the sources are the project packages the schemas are
// built from (embedded and named non-struct types included), plus go/go.mod
// for a dependency's type; unrelated packages are not sources.
func TestSchemaSources(t *testing.T) {
	_, _, sources, err := schemaProject(t, map[string]string{
		"obj/user.go":     objUser,
		"roles/role.go":   "package roles\n\ntype Role string\n",
		"base/base.go":    "package base\n\ntype Base struct {\n\tX int `json:\"x\"`\n}\n",
		"unused/thing.go": "package unused\n\ntype Thing struct{}\n",
		"api/e.go": strings.Replace(apiEndpoint("PUT", "Req", "Empty", `
type Req struct {
	base.Base
	Role  roles.Role `+"`json:\"role\"`"+`
	Price dep.Money  `+"`json:\"price\"`"+`
	Addr  obj.Address `+"`json:\"addr\"`"+`
	Thing unused.Thing `+"`json:\"-\"`"+`
}
`), `"example.com/dep"`, `"example.com/dep"
	"example.com/app/base"
	"example.com/app/roles"
	"example.com/app/unused"`, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sources, ","); got != "go/base/*.go,go/go.mod,go/obj/*.go,go/roles/*.go" {
		t.Errorf("sources = %q", got)
	}
}
