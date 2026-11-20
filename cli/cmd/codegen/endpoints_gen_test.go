package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// With no endpoints, the generated file must still compile: Provider() is
// an empty function, with no unused import.
func TestRenderEndpointsGenNoEndpointsCompiles(t *testing.T) {
	src, err := renderEndpointsGen(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "func Provider()") {
		t.Fatalf("generated file has no Provider func:\n%s", src)
	}
	compileGenerated(t, map[string]string{"api/endpoints.gen.go": string(src)})
}

func TestRenderEndpointsGenRegistersEndpoint(t *testing.T) {
	src, err := renderEndpointsGen([]endpointInfo{{StructName: "Ping", ReqType: "Req", RespType: "Resp"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "endpoints.gen.go", src, 0); err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(string(src), `import schemafapi "github.com/flocko-motion/schemaf/api"`) {
		t.Fatalf("generated file lacks the schemafapi import:\n%s", src)
	}
	if !strings.Contains(string(src), "schemafapi.NewRoute[Req, Resp](Ping{}") {
		t.Fatalf("generated file does not register Ping:\n%s", src)
	}
}
