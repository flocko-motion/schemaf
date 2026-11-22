package codegen

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// specOf builds the OpenAPI spec of a schemaProject result.
func specOf(t *testing.T, eps []endpointInfo, schemas map[string]any) staticSpec {
	t.Helper()
	raw, err := buildOpenAPISpec("myapp", eps, schemas)
	if err != nil {
		t.Fatal(err)
	}
	var spec staticSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

// wantErrs fails unless err contains every string in wants.
func wantErrs(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("no error")
	}
	msg := err.Error()
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("error:\n%s", msg)
	}
}

// TestOpenAPIEmbeddedParams: an embedded struct's path and query fields
// count as the request type's own, at any depth, in field order, including
// a generic embedded struct and an unexported one; the JSON body keeps only
// the JSON fields.
func TestOpenAPIEmbeddedParams(t *testing.T) {
	src := paramsHeader + `
type Deeper struct {
	Offset *int ` + "`query:\"offset\"`" + `
}

type PageParams struct {
	Limit *int ` + "`query:\"limit\"`" + `
	Deeper
}

type ListParams[S ~string] struct {
	SortBy *S ` + "`query:\"sortBy\"`" + `
}

type tenant struct {
	Tenant string ` + "`path:\"tenant\"`" + `
}

type Extra struct {
	Mine *bool ` + "`query:\"mine\"`" + `
}

type ListUsersReq struct {
	tenant
	PageParams
	ListParams[Role]
	Extra  ` + "`json:\",omitempty\"`" + `
	Search *string ` + "`query:\"search\"`" + `
}

type CreateUserReq struct {
	tenant
	Name string ` + "`json:\"name\"`" + `
}
` + paramsEndpoint("ListUsersEndpoint", "GET", "/api/{tenant}/users", "ListUsersReq") +
		paramsEndpoint("CreateUserEndpoint", "POST", "/api/{tenant}/users", "CreateUserReq")

	eps, schemas, _, err := schemaProject(t, map[string]string{"api/users.go": src})
	if err != nil {
		t.Fatal(err)
	}
	spec := specOf(t, eps, schemas)
	want := `[` +
		`{"in":"path","name":"tenant","required":true,"schema":{"type":"string"}},` +
		`{"in":"query","name":"limit","required":false,"schema":{"type":"integer"}},` +
		`{"in":"query","name":"offset","required":false,"schema":{"type":"integer"}},` +
		`{"in":"query","name":"sortBy","required":false,"schema":{"type":"string"}},` +
		`{"in":"query","name":"mine","required":false,"schema":{"type":"boolean"}},` +
		`{"in":"query","name":"search","required":false,"schema":{"type":"string"}}]`
	if got := jsonOf(t, spec.Paths["/api/{tenant}/users"]["get"]["parameters"]); got != want {
		t.Errorf("list params =\n %s\nwant\n %s", got, want)
	}
	if got := jsonOf(t, schemas["CreateUserReq"]); got != `{"properties":{"name":{"type":"string"}},"required":["name"],"type":"object"}` {
		t.Errorf("CreateUserReq schema = %s", got)
	}
}

// TestOpenAPIEmbeddedParamErrors: a pointer embed with parameters, the same
// parameter name across embeds, parameters inside a json-named embed and an
// untagged field inside an embed of a GET request are codegen errors.
func TestOpenAPIEmbeddedParamErrors(t *testing.T) {
	src := paramsHeader + `
type Paging struct {
	Page *int ` + "`query:\"page\"`" + `
}

type PtrEmbedReq struct {
	*Paging
}

type DupQueryReq struct {
	Paging
	Page *int ` + "`query:\"page\"`" + `
}

type IDParam struct {
	ID string ` + "`path:\"id\"`" + `
}

type DupPathReq struct {
	IDParam
	Other string ` + "`path:\"id\"`" + `
}

type NamedEmbedReq struct {
	Paging ` + "`json:\"paging\"`" + `
	Title string ` + "`json:\"title\"`" + `
}

type DashEmbedReq struct {
	Paging ` + "`json:\"-\"`" + `
	Title string ` + "`json:\"title\"`" + `
}

type Sorting struct {
	Page *string ` + "`query:\"page\"`" + `
}

type SiblingDupReq struct {
	Paging
	Sorting
}

type Filters struct {
	Search string
}

type UntaggedEmbedReq struct {
	Filters
}
` + paramsEndpoint("PtrEmbedEndpoint", "GET", "/api/ptr", "PtrEmbedReq") +
		paramsEndpoint("DupQueryEndpoint", "GET", "/api/dupq", "DupQueryReq") +
		paramsEndpoint("DupPathEndpoint", "GET", "/api/dupp/{id}", "DupPathReq") +
		paramsEndpoint("NamedEmbedEndpoint", "POST", "/api/named", "NamedEmbedReq") +
		paramsEndpoint("UntaggedEmbedEndpoint", "GET", "/api/untagged", "UntaggedEmbedReq") +
		paramsEndpoint("DashEmbedEndpoint", "POST", "/api/dash", "DashEmbedReq") +
		paramsEndpoint("SiblingDupEndpoint", "GET", "/api/sibling", "SiblingDupReq")

	_, _, _, err := schemaProject(t, map[string]string{"api/bad.go": src})
	wantErrs(t, err,
		"endpoint PtrEmbedEndpoint: request type PtrEmbedReq, field Paging: embedded pointer to Paging, which has `path` or `query` fields — embed it by value",
		"endpoint DupQueryEndpoint: request type DupQueryReq, field Page: field Paging.Page is already tagged `query:\"page\"`",
		"endpoint DupPathEndpoint: request type DupPathReq, field Other: field IDParam.ID is already tagged `path:\"id\"`",
		"endpoint NamedEmbedEndpoint: request type NamedEmbedReq, field Paging: struct Paging is embedded with the json name \"paging\", so it is not part of the request type and its `path` and `query` fields are never read — remove the json name",
		"endpoint DashEmbedEndpoint: request type DashEmbedReq, field Paging: struct Paging is embedded with the json name \"-\"",
		"endpoint SiblingDupEndpoint: request type SiblingDupReq, field Sorting.Page: field Paging.Page is already tagged `query:\"page\"`",
		"endpoint UntaggedEmbedEndpoint: request type UntaggedEmbedReq, field Filters.Search: a GET request has no body",
	)
}

// TestOpenAPIGenericSchemas: each instance of a generic struct gets a
// component named from its type arguments and its own name, also when the
// instance appears directly in the Handle signature.
func TestOpenAPIGenericSchemas(t *testing.T) {
	src := `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type Page[T any] struct {
	Items []T ` + "`json:\"items\"`" + `
	Total int ` + "`json:\"total\"`" + `
}

type Pair[A, B any] struct {
	First  A ` + "`json:\"first\"`" + `
	Second B ` + "`json:\"second\"`" + `
}

type Address = obj.Address

type Resp struct {
	Users     Page[*obj.Address]                 ` + "`json:\"users\"`" + `
	Names     Page[string]                       ` + "`json:\"names\"`" + `
	Pairs     Page[Pair[obj.User, Address]]      ` + "`json:\"pairs\"`" + `
	ByAddress Pair[Address, obj.User]            ` + "`json:\"byAddress\"`" + `
}

type Req struct{}

type ListResp = Page[obj.User]

type ListEndpoint struct{}

func (ListEndpoint) Method() string             { return "GET" }
func (ListEndpoint) Path() string               { return "/api/list" }
func (ListEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (ListEndpoint) Handle(ctx context.Context, req Req) (ListResp, error) { return ListResp{}, nil }

type DirectEndpoint struct{}

func (DirectEndpoint) Method() string             { return "GET" }
func (DirectEndpoint) Path() string               { return "/api/direct" }
func (DirectEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (DirectEndpoint) Handle(ctx context.Context, req Req) (Pair[obj.Address, obj.Role], error) {
	return Pair[obj.Address, obj.Role]{}, nil
}

type AllEndpoint struct{}

func (AllEndpoint) Method() string             { return "GET" }
func (AllEndpoint) Path() string               { return "/api/all" }
func (AllEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (AllEndpoint) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`
	eps, schemas, _, err := schemaProject(t, map[string]string{"obj/user.go": objUser, "api/list.go": src})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]endpointInfo{}
	for _, ep := range eps {
		byName[ep.StructName] = ep
	}
	if got := jsonOf(t, byName["ListEndpoint"].RespSchema); got != `{"$ref":"#/components/schemas/UserPage"}` {
		t.Errorf("List response = %s", got)
	}
	if got := jsonOf(t, byName["DirectEndpoint"].RespSchema); got != `{"$ref":"#/components/schemas/AddressRolePair"}` {
		t.Errorf("Direct response = %s", got)
	}
	if got := jsonOf(t, schemas["UserPage"]); got != `{"properties":{"items":{"items":{"$ref":"#/components/schemas/User"},"type":"array"},"total":{"type":"integer"}},"required":["items","total"],"type":"object"}` {
		t.Errorf("UserPage = %s", got)
	}
	if got := jsonOf(t, schemas["StringPage"]); got != `{"properties":{"items":{"items":{"type":"string"},"type":"array"},"total":{"type":"integer"}},"required":["items","total"],"type":"object"}` {
		t.Errorf("StringPage = %s", got)
	}
	if got := jsonOf(t, schemas["Resp"]); got != `{"properties":{`+
		`"byAddress":{"$ref":"#/components/schemas/AddressUserPair"},`+
		`"names":{"$ref":"#/components/schemas/StringPage"},`+
		`"pairs":{"$ref":"#/components/schemas/UserAddressPairPage"},`+
		`"users":{"$ref":"#/components/schemas/AddressPage"}},`+
		`"required":["users","names","pairs","byAddress"],"type":"object"}` {
		t.Errorf("Resp = %s", got)
	}
	for _, name := range []string{"AddressPage", "UserAddressPair", "AddressUserPair", "AddressRolePair", "UserAddressPairPage"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("no component %s; have %v", name, keysOf(schemas))
		}
	}
}

// keysOf returns m's keys, sorted.
func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestOpenAPIGenericErrors: an unnamed type argument, and a generated name
// that collides with a plain type or another instance, are codegen errors.
func TestOpenAPIGenericErrors(t *testing.T) {
	dep := `package other

type User struct {
	Nick string ` + "`json:\"nick\"`" + `
}
`
	src := `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
	"example.com/app/other"
)

type Page[T any] struct {
	Items []T ` + "`json:\"items\"`" + `
}

type StringPage struct {
	X int ` + "`json:\"x\"`" + `
}

type Resp struct {
	Slices  Page[[]obj.User]        ` + "`json:\"slices\"`" + `
	Maps    Page[map[string]int]    ` + "`json:\"maps\"`" + `
	Strings Page[string]            ` + "`json:\"strings\"`" + `
	Plain   StringPage              ` + "`json:\"plain\"`" + `
	A       Page[obj.Address]       ` + "`json:\"a\"`" + `
	B       Page[other.User]        ` + "`json:\"b\"`" + `
	C       Page[obj.User]          ` + "`json:\"c\"`" + `
	D       Page[*obj.Address]      ` + "`json:\"d\"`" + `
}

type Req struct{}

type E struct{}

func (E) Method() string             { return "GET" }
func (E) Path() string               { return "/api/e" }
func (E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (E) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`
	_, _, _, err := schemaProject(t, map[string]string{"obj/user.go": objUser, "other/user.go": dep, "api/e.go": src})
	wantErrs(t, err,
		"Resp.Slices: generic type example.com/app/api.Page[[]example.com/app/obj.User] has the type argument []example.com/app/obj.User, which has no name — use a named type as the type argument",
		"Resp.Maps: generic type example.com/app/api.Page[map[string]int] has the type argument map[string]int, which has no name",
		`OpenAPI schema name "StringPage" is used by both`,
		"example.com/app/api.Page[string]",
		"example.com/app/api.StringPage",
		`OpenAPI schema name "UserPage" is used by both`,
		"example.com/app/api.Page[example.com/app/other.User]",
		"example.com/app/api.Page[example.com/app/obj.User]",
		`OpenAPI schema name "AddressPage" is used by both example.com/app/api.Page[example.com/app/obj.Address] and example.com/app/api.Page[*example.com/app/obj.Address]`,
	)
}

// TestOpenAPIEnums: a named string type with constants becomes a component
// with enum, referenced from parameters, JSON fields and slice items;
// without constants, implementing TextMarshaler, or over an integer it
// stays inline.
func TestOpenAPIEnums(t *testing.T) {
	objSort := `package obj

type Filter string

const (
	FilterActive  Filter = "active"
	FilterBlocked Filter = "blocked"
)

const NotAFilter = "nope"
`
	src := `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type UserSort string

const (
	UserSortName      UserSort = "name"
	UserSortCreatedAt UserSort = "createdAt"
	UserSortDefault            = UserSortName
	userSortSecret    UserSort = "secret"
)

type Free string

type Coded string

func (c Coded) MarshalText() ([]byte, error) { return []byte(c), nil }

const CodedA Coded = "a"

type Level int

const LevelHigh Level = 1

type ListReq struct {
	Kind    UserSort     ` + "`path:\"kind\"`" + `
	SortBy  *UserSort    ` + "`query:\"sortBy\"`" + `
	Filter  []obj.Filter ` + "`query:\"filter\"`" + `
	Free    *Free        ` + "`query:\"free\"`" + `
}

type Resp struct {
	Sort    UserSort     ` + "`json:\"sort\"`" + `
	Filters []obj.Filter ` + "`json:\"filters\"`" + `
	Free    Free         ` + "`json:\"free\"`" + `
	Coded   Coded        ` + "`json:\"coded\"`" + `
	Level   Level        ` + "`json:\"level\"`" + `
}

type E struct{}

func (E) Method() string             { return "GET" }
func (E) Path() string               { return "/api/users/{kind}" }
func (E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (E) Handle(ctx context.Context, req ListReq) (Resp, error) { return Resp{}, nil }

type SortedReq[S ~string] struct {
	SortBy *S ` + "`query:\"sortBy\"`" + `
}

type G struct{}

func (G) Method() string             { return "GET" }
func (G) Path() string               { return "/api/sorted" }
func (G) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (G) Handle(ctx context.Context, req SortedReq[UserSort]) (Resp, error) { return Resp{}, nil }
`
	eps, schemas, sources, err := schemaProject(t, map[string]string{"obj/user.go": objUser, "obj/filter.go": objSort, "api/e.go": src})
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonOf(t, schemas["UserSort"]); got != `{"enum":["name","createdAt","secret"],"type":"string"}` {
		t.Errorf("UserSort = %s", got)
	}
	if got := jsonOf(t, schemas["Filter"]); got != `{"enum":["active","blocked"],"type":"string"}` {
		t.Errorf("Filter = %s", got)
	}
	for _, name := range []string{"Free", "Coded", "Level"} {
		if _, ok := schemas[name]; ok {
			t.Errorf("%s got a component", name)
		}
	}
	spec := specOf(t, eps, schemas)
	want := `[` +
		`{"in":"path","name":"kind","required":true,"schema":{"$ref":"#/components/schemas/UserSort"}},` +
		`{"in":"query","name":"sortBy","required":false,"schema":{"$ref":"#/components/schemas/UserSort"}},` +
		`{"explode":true,"in":"query","name":"filter","required":false,"schema":{"items":{"$ref":"#/components/schemas/Filter"},"type":"array"},"style":"form"},` +
		`{"in":"query","name":"free","required":false,"schema":{"type":"string"}}]`
	if got := jsonOf(t, spec.Paths["/api/users/{kind}"]["get"]["parameters"]); got != want {
		t.Errorf("params =\n %s\nwant\n %s", got, want)
	}
	// A generic request type, straight in the Handle signature, with the
	// enum as its type argument.
	if got := jsonOf(t, spec.Paths["/api/sorted"]["get"]["parameters"]); got != `[{"in":"query","name":"sortBy","required":false,"schema":{"$ref":"#/components/schemas/UserSort"}}]` {
		t.Errorf("sorted params = %s", got)
	}
	if got := jsonOf(t, schemas["Resp"]); got != `{"properties":{`+
		`"coded":{"type":"string"},`+
		`"filters":{"items":{"$ref":"#/components/schemas/Filter"},"type":"array"},`+
		`"free":{"type":"string"},`+
		`"level":{"type":"integer"},`+
		`"sort":{"$ref":"#/components/schemas/UserSort"}},`+
		`"required":["sort","filters","free","coded","level"],"type":"object"}` {
		t.Errorf("Resp = %s", got)
	}
	// The constants live in obj: a change there must rerun codegen.
	if !slices.Contains(sources, "go/obj/*.go") {
		t.Errorf("sources = %v, want go/obj/*.go", sources)
	}
}

// TestOpenAPIEnumNameCollision: an enum's component name is checked like any
// other.
func TestOpenAPIEnumNameCollision(t *testing.T) {
	objSort := `package obj

type Sort string

const SortName Sort = "name"
`
	src := `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type Sort struct {
	X int ` + "`json:\"x\"`" + `
}

type Resp struct {
	A obj.Sort ` + "`json:\"a\"`" + `
	B Sort     ` + "`json:\"b\"`" + `
}

type Req struct{}

type E struct{}

func (E) Method() string             { return "GET" }
func (E) Path() string               { return "/api/e" }
func (E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (E) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`
	_, _, _, err := schemaProject(t, map[string]string{"obj/sort.go": objSort, "api/e.go": src})
	wantErrs(t, err, `OpenAPI schema name "Sort" is used by both example.com/app/obj.Sort and example.com/app/api.Sort`)
}
