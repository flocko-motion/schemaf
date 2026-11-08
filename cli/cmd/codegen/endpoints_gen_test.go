package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// With no endpoints, the generated file must still compile: Provider() is
// an empty function. The type-check also catches an unused import.
func TestRenderEndpointsGenNoEndpointsCompiles(t *testing.T) {
	src, err := renderEndpointsGen(nil)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "endpoints.gen.go", src, 0)
	if err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, src)
	}
	if _, err := (&types.Config{}).Check("api", fset, []*ast.File{file}, nil); err != nil {
		t.Fatalf("generated file does not type-check: %v\n%s", err, src)
	}
	if file.Scope.Lookup("Provider") == nil {
		t.Fatalf("generated file has no Provider func:\n%s", src)
	}
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
