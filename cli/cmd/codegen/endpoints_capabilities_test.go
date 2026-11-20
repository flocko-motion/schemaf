package codegen

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const capHeader = scanHeader + `
type Event struct{ Seq int ` + "`json:\"seq\"`" + ` }
`

func TestScanStreamEndpoint(t *testing.T) {
	eps, err := scanSource(t, capHeader+`
// EventsEndpoint streams events.
type EventsEndpoint struct{}
func (EventsEndpoint) Method() string { return "GET" }
func (EventsEndpoint) Path() string { return "/api/events" }
func (EventsEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthRequired }
func (EventsEndpoint) Stream(ctx context.Context, req Req) (<-chan Event, error) { return nil, nil }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || !eps[0].IsStream || eps[0].ReqType != "Req" || eps[0].RespType != "Event" {
		t.Fatalf("eps = %+v", eps)
	}
}

func TestScanContentTypes(t *testing.T) {
	eps, err := scanSource(t, capHeader+`
type PutEndpoint struct{}
func (PutEndpoint) Method() string { return "PUT" }
func (PutEndpoint) Path() string { return "/api/games" }
func (PutEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (PutEndpoint) Consumes() []string { return []string{"application/x-yaml", "application/json"} }
func (PutEndpoint) Produces() []string { return []string{"application/yaml"} }
func (PutEndpoint) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }

type PlainEndpoint struct{}
func (PlainEndpoint) Method() string { return "POST" }
func (PlainEndpoint) Path() string { return "/api/plain" }
func (PlainEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (PlainEndpoint) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].StructName < eps[j].StructName })
	plain, put := eps[0], eps[1]
	if strings.Join(put.Consumes, ",") != "application/x-yaml,application/json" || strings.Join(put.Produces, ",") != "application/yaml" {
		t.Errorf("PutEndpoint consumes %v, produces %v", put.Consumes, put.Produces)
	}
	if strings.Join(plain.Consumes, ",") != "application/json" || strings.Join(plain.Produces, ",") != "application/json" {
		t.Errorf("PlainEndpoint consumes %v, produces %v (want the JSON default)", plain.Consumes, plain.Produces)
	}
}

func TestScanCapabilityErrors(t *testing.T) {
	const okPath = `func (e E) Path() string { return "/api/e" }
func (e E) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
`
	const get = `func (e E) Method() string { return "GET" }
`
	const post = `func (e E) Method() string { return "POST" }
`
	const handle = `func (e E) Handle(ctx context.Context, req Req) (Resp, error) { return Resp{}, nil }
`
	const raw = `func (e E) HandleRaw(w http.ResponseWriter, r *http.Request) error { return nil }
`
	const stream = `func (e E) Stream(ctx context.Context, req Req) (<-chan Event, error) { return nil, nil }
`
	cases := []struct{ name, body, want string }{
		{"stream not GET", post + okPath + stream, "endpoint E: a stream endpoint's Method() must be \"GET\""},
		{"stream send-only channel", get + okPath +
			`func (e E) Stream(ctx context.Context, req Req) (chan<- Event, error) { return nil, nil }`,
			"endpoint E: Stream must have the signature `Stream(ctx context.Context, req Req) (<-chan Event, error)`"},
		{"stream no error", get + okPath +
			`func (e E) Stream(ctx context.Context, req Req) <-chan Event { return nil }`,
			"endpoint E: Stream must have the signature"},
		{"Handle and Stream", get + okPath + handle + stream, "endpoint E has more than one of Handle, HandleRaw and Stream"},
		{"HandleRaw and Stream", get + okPath + raw + stream, "endpoint E has more than one of Handle, HandleRaw and Stream"},
		{"no handler", get + okPath, "endpoint E has Method(), Path() and Auth() but no Handle, HandleRaw or Stream method"},
		{"unsupported media type", post + okPath + handle +
			`func (e E) Produces() []string { return []string{"text/csv"} }`,
			`endpoint E: Produces(): unsupported media type "text/csv"`},
		{"duplicate media type", post + okPath + handle +
			`func (e E) Consumes() []string { return []string{"application/json", "application/json"} }`,
			`endpoint E: Consumes(): media type "application/json" listed twice`},
		{"empty list", post + okPath + handle +
			`func (e E) Consumes() []string { return []string{} }`,
			"endpoint E: Consumes() returns no media types"},
		{"not a literal", post + okPath + handle +
			`var types = []string{"application/json"}
func (e E) Produces() []string { return types }`,
			"endpoint E: Produces() must return a []string literal of string literals"},
		{"Consumes on GET", get + okPath + handle +
			`func (e E) Consumes() []string { return []string{"application/json"} }`,
			"endpoint E: Consumes() on a GET endpoint: GET requests carry no body"},
		{"Produces on stream", get + okPath + stream +
			`func (e E) Produces() []string { return []string{"application/json"} }`,
			"endpoint E: Produces() is only allowed on endpoints with Handle"},
		{"Consumes on raw", post + okPath + raw +
			`func (e E) Consumes() []string { return []string{"application/json"} }`,
			"endpoint E: Consumes() is only allowed on endpoints with Handle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eps, err := scanSource(t, capHeader+"\ntype E struct{}\n"+tc.body+"\n")
			if err == nil {
				t.Fatalf("no error (endpoints %+v)", eps)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q\nwant it to contain %q", err, tc.want)
			}
		})
	}
}

func TestOpenAPIListsMediaTypesAndStreams(t *testing.T) {
	eps, schemas, _, err := schemaProject(t, map[string]string{
		"obj/user.go": objUser,
		"api/caps.go": `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
	"example.com/app/obj"
)

type GameReq struct {
	ID string ` + "`path:\"id\"`" + `
}

type Game struct {
	Title string ` + "`json:\"title\"`" + `
}

type PutGameEndpoint struct{}

func (PutGameEndpoint) Method() string             { return "PUT" }
func (PutGameEndpoint) Path() string               { return "/api/games" }
func (PutGameEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (PutGameEndpoint) Consumes() []string         { return []string{"application/x-yaml", "application/json"} }
func (PutGameEndpoint) Handle(ctx context.Context, req Game) (Game, error) { return req, nil }

type GetGameEndpoint struct{}

func (GetGameEndpoint) Method() string             { return "GET" }
func (GetGameEndpoint) Path() string               { return "/api/games/{id}" }
func (GetGameEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (GetGameEndpoint) Produces() []string         { return []string{"application/x-yaml", "application/json"} }
func (GetGameEndpoint) Handle(ctx context.Context, req GameReq) (Game, error) { return Game{}, nil }

// UserEventsEndpoint streams users from another package.
type UserEventsEndpoint struct{}

func (UserEventsEndpoint) Method() string             { return "GET" }
func (UserEventsEndpoint) Path() string               { return "/api/users/{id}/events" }
func (UserEventsEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (UserEventsEndpoint) Stream(ctx context.Context, req GameReq) (<-chan obj.User, error) {
	return nil, nil
}
`,
	})
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
	keys := func(v any) string {
		var ks []string
		for k := range v.(map[string]any) {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	content := func(path, method string, request bool) map[string]any {
		op := spec.Paths[path][method]
		if request {
			return op["requestBody"].(map[string]any)["content"].(map[string]any)
		}
		return op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	}
	if got := keys(content("/api/games", "put", true)); got != "application/json,application/x-yaml" {
		t.Errorf("PUT request content: %s", got)
	}
	if got := keys(content("/api/games", "put", false)); got != "application/json" {
		t.Errorf("PUT response content: %s", got)
	}
	if got := keys(content("/api/games/{id}", "get", false)); got != "application/json,application/x-yaml" {
		t.Errorf("GET response content: %s", got)
	}
	if _, ok := spec.Paths["/api/users/{id}/events"]["get"]["requestBody"]; ok {
		t.Error("stream has a request body")
	}
	ev := content("/api/users/{id}/events", "get", false)
	if got := keys(ev); got != "text/event-stream" {
		t.Fatalf("stream response content: %s", got)
	}
	if ref := ev["text/event-stream"].(map[string]any)["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/User" {
		t.Errorf("stream schema $ref = %v", ref)
	}
	if _, ok := spec.Components.Schemas["User"]; !ok {
		t.Error("User (event) schema missing from components")
	}
}

func TestRenderEndpointsGenRegistersStream(t *testing.T) {
	src, err := renderEndpointsGen([]endpointInfo{{StructName: "Events", ReqType: "Req", RespType: "Event", IsStream: true}}, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "schemafapi.NewStreamRoute[Req, Event](Events{}") {
		t.Fatalf("generated file does not register the stream:\n%s", src)
	}
	compileGenerated(t, map[string]string{
		"api/endpoints.gen.go": string(src),
		"api/events.go": `package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

type Req struct{}
type Event struct{ Seq int }
type Events struct{}

func (Events) Method() string                { return "GET" }
func (Events) Path() string                  { return "/api/events" }
func (Events) Auth() schemafapi.AuthLevel    { return schemafapi.AuthNone }
func (Events) Stream(ctx context.Context, req Req) (<-chan Event, error) { return nil, nil }
`,
	})
}
