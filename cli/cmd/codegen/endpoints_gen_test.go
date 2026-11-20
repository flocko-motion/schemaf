package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// With no endpoints, the generated file must still compile: Provider() is
// an empty function, with no unused import.
func TestRenderEndpointsGenNoEndpointsCompiles(t *testing.T) {
	src, err := renderEndpointsGen(nil, []byte(`{"openapi": "3.0.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "func Provider()") {
		t.Fatalf("generated file has no Provider func:\n%s", src)
	}
	compileGenerated(t, map[string]string{"api/endpoints.gen.go": string(src)})
}

func TestRenderEndpointsGenRegistersEndpoint(t *testing.T) {
	src, err := renderEndpointsGen([]endpointInfo{{StructName: "Ping", ReqType: "Req", RespType: "Resp"}}, []byte("{}"))
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

// The generated Provider() hands the spec to schemaf byte for byte, whatever
// characters it contains, and the file still compiles.
func TestRenderEndpointsGenCarriesSpec(t *testing.T) {
	spec := "{\n  \"description\": \"quotes \\\" backticks ` and ${x} \u00e4\"\n}"
	src, err := renderEndpointsGen(nil, []byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "schemafapi.SetOpenAPISpec(openAPISpec)") {
		t.Fatalf("Provider does not set the spec:\n%s", src)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "endpoints.gen.go", src, 0)
	if err != nil {
		t.Fatalf("generated file does not parse: %v\n%s", err, src)
	}
	var got string
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			lit := gd.Specs[0].(*ast.ValueSpec).Values[0].(*ast.BasicLit)
			if got, err = strconv.Unquote(lit.Value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got != spec {
		t.Errorf("openAPISpec = %q, want %q", got, spec)
	}
	compileGenerated(t, map[string]string{"api/endpoints.gen.go": string(src)})
}
