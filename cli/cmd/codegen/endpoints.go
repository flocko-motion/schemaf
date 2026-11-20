// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// NOTE: UNTESTED draft — new code, not yet exercised end-to-end.

package codegen

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/flocko-motion/schemaf/api"
	cli "github.com/flocko-motion/schemaf/cli"
)

//go:embed endpoints.gen.go.tmpl
var endpointsGenTemplate string

const (
	apiDir          = "go/api"
	endpointsGenOut = apiDir + "/endpoints.gen.go"
	openapiJSONOut  = "gen/openapi.json"
	// schemaSourcesFile lists, one glob per line, the packages outside go/api
	// that declare a type the spec's schemas are built from (and go/go.mod for
	// dependencies). They are codegen inputs: see inputs.go.
	schemaSourcesFile = "gen/schema-sources"
)

// endpointInfo holds everything extracted from a single endpoint struct.
type endpointInfo struct {
	StructName  string
	Method      string
	Path        string
	Auth        api.AuthLevel
	ReqType     string // empty for raw endpoints
	RespType    string // empty for raw endpoints; the event type of a stream
	Summary     string
	Description string
	IsRaw       bool
	IsStream    bool
	Consumes    []string       // request body media types (Handle endpoints)
	Produces    []string       // response body media types (Handle endpoints)
	ReqSchema   map[string]any // request body schema; nil for no body
	RespSchema  map[string]any // 200 response schema (a stream's: each event's data); nil for no content
}

func runEndpointsGen(_ *cli.Context) error {
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", apiDir, err)
	}

	projectName, err := readProjectName()
	if err != nil {
		return fmt.Errorf("reading project name: %w", err)
	}

	endpoints, err := scanAPIDir(apiDir)
	if err != nil {
		return fmt.Errorf("scanning %s: %w", apiDir, err)
	}

	if len(endpoints) == 0 {
		warnEmptyInput("no endpoints in %s — Provider() registers nothing", apiDir)
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].StructName < endpoints[j].StructName
	})

	schemas, sources, err := loadSchemas(apiDir, endpoints)
	if err != nil {
		return err
	}
	spec, err := buildOpenAPISpec(projectName, endpoints, schemas)
	if err != nil {
		return err
	}

	// endpoints.gen.go carries the spec: the server serves exactly gen/openapi.json.
	if err := writeEndpointsGen(endpoints, spec); err != nil {
		return err
	}
	if err := writeGenFile(openapiJSONOut, spec); err != nil {
		return err
	}
	if err := writeGenFile(schemaSourcesFile, []byte(strings.Join(append(sources, ""), "\n"))); err != nil {
		return err
	}

	cli.Success("endpoints codegen complete: %d endpoint(s)", len(endpoints))
	return nil
}

// scanAPIDir parses the package's own sources in apiDir — every Go file
// except *.gen.go and *_test.go — and extracts the endpoint structs. An
// endpoint struct in a test file belongs to the test (package api or
// api_test) and is never registered. The schemas of their types come from
// loadSchemas.
func scanAPIDir(dir string) ([]endpointInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		name := fi.Name()
		return !strings.HasSuffix(name, ".gen.go") && !strings.HasSuffix(name, "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", dir, err)
	}

	// Collect all method declarations and struct types across files
	methods := map[methodKey]methodDecl{}
	structNames := map[string]bool{}
	docComments := map[string]string{} // struct name → doc comment

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil || len(d.Recv.List) == 0 {
						continue
					}
					recv := recvTypeName(d.Recv.List[0].Type)
					if recv == "" {
						continue
					}
					methods[methodKey{recv, d.Name.Name}] = methodDecl{fn: d, file: file}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if _, ok := ts.Type.(*ast.StructType); !ok {
							continue
						}
						structNames[ts.Name.Name] = true
						// Extract doc comment
						comment := ""
						if d.Doc != nil {
							comment = d.Doc.Text()
						} else if ts.Comment != nil {
							comment = ts.Comment.Text()
						}
						docComments[ts.Name.Name] = strings.TrimSpace(comment)
					}
				}
			}
		}
	}

	names := make([]string, 0, len(structNames))
	for name := range structNames {
		names = append(names, name)
	}
	sort.Strings(names)

	// Identify endpoint structs. A struct with Handle or HandleRaw and any of
	// Method, Path, Auth has the endpoint shape: it is either a valid
	// endpoint or a codegen error, never silently skipped.
	var endpoints []endpointInfo
	var errs []error
	for _, name := range names {
		ep, isEndpoint, err := parseEndpoint(name, methods)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !isEndpoint {
			continue
		}
		ep.Summary, ep.Description = splitDocComment(docComments[name], name)
		endpoints = append(endpoints, ep)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return endpoints, nil
}

type methodKey struct{ recv, name string }

// methodDecl is a method declaration and the file it is declared in (for
// resolving import aliases).
type methodDecl struct {
	fn   *ast.FuncDecl
	file *ast.File
}

const schemafAPIImport = "github.com/flocko-motion/schemaf/api"

// parseEndpoint checks whether struct name has the endpoint shape and, if
// so, extracts it. isEndpoint is false only for structs that are clearly not
// endpoints (no Handle/HandleRaw, or none of Method/Path/Auth).
func parseEndpoint(name string, methods map[methodKey]methodDecl) (ep endpointInfo, isEndpoint bool, err error) {
	methodFn, hasMethod := methods[methodKey{name, "Method"}]
	pathFn, hasPath := methods[methodKey{name, "Path"}]
	authFn, hasAuth := methods[methodKey{name, "Auth"}]
	handleFn, hasHandle := methods[methodKey{name, "Handle"}]
	rawFn, hasHandleRaw := methods[methodKey{name, "HandleRaw"}]
	streamFn, hasStream := methods[methodKey{name, "Stream"}]

	if !hasHandle && !hasHandleRaw && !hasStream {
		if hasMethod && hasPath && hasAuth {
			// Everything but the handler: almost certainly a misnamed Handle
			// (e.g. Handler, HandleRAW). Don't let the endpoint vanish.
			return ep, false, fmt.Errorf("endpoint %s has Method(), Path() and Auth() but no Handle, HandleRaw or Stream method: add `Handle(ctx context.Context, req Req) (Resp, error)`, `HandleRaw(w http.ResponseWriter, r *http.Request) error` or `Stream(ctx context.Context, req Req) (<-chan Event, error)`", name)
		}
		return ep, false, nil
	}
	if !hasMethod && !hasPath && !hasAuth {
		return ep, false, nil
	}
	var missing []string
	for _, m := range []struct {
		name string
		ok   bool
	}{{"Method()", hasMethod}, {"Path()", hasPath}, {"Auth()", hasAuth}} {
		if !m.ok {
			missing = append(missing, m.name)
		}
	}
	if len(missing) > 0 {
		return ep, false, fmt.Errorf("endpoint %s is missing %s: an endpoint needs Method(), Path(), Auth() and one of Handle, HandleRaw, Stream", name, strings.Join(missing, ", "))
	}
	if n := countTrue(hasHandle, hasHandleRaw, hasStream); n > 1 {
		return ep, false, fmt.Errorf("endpoint %s has more than one of Handle, HandleRaw and Stream — exactly one is allowed", name)
	}

	ep.StructName = name
	if ep.Method, err = parseStringMethod(name, methodFn); err != nil {
		return ep, false, err
	}
	if ep.Path, err = parseStringMethod(name, pathFn); err != nil {
		return ep, false, err
	}
	// The spec gates /api/ at runtime; every endpoint lives under it.
	if !strings.HasPrefix(ep.Path, "/api/") {
		return ep, false, fmt.Errorf("endpoint %s: Path() %q must be under /api/ (e.g. \"/api%s\"): the server only routes /api/ to endpoints, everything else is the frontend", name, ep.Path, ep.Path)
	}
	if seg, ok := api.UnsupportedWildcard(ep.Path); ok {
		return ep, false, fmt.Errorf("endpoint %s: Path() %q uses %s, which OpenAPI can't express — use one {param} per segment", name, ep.Path, seg)
	}
	if ep.Auth, err = parseAuthMethod(name, authFn); err != nil {
		return ep, false, err
	}
	switch {
	case hasHandleRaw:
		ep.IsRaw = true
		if err := checkHandleRaw(name, rawFn); err != nil {
			return ep, false, err
		}
	case hasStream:
		ep.IsStream = true
		if ep.Method != "GET" {
			return ep, false, fmt.Errorf("endpoint %s: a stream endpoint's Method() must be \"GET\" (browsers open streams with EventSource), got %q", name, ep.Method)
		}
		if ep.ReqType, ep.RespType, err = parseStream(name, streamFn); err != nil {
			return ep, false, err
		}
	default:
		if ep.ReqType, ep.RespType, err = parseHandle(name, handleFn); err != nil {
			return ep, false, err
		}
	}
	if err := parseContentTypes(&ep, methods); err != nil {
		return ep, false, err
	}
	return ep, true, nil
}

func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// parseContentTypes reads the optional Consumes() and Produces() of ep (JSON
// when absent). They are only allowed on Handle endpoints, and Consumes()
// only on methods that carry a body.
func parseContentTypes(ep *endpointInfo, methods map[methodKey]methodDecl) error {
	for _, m := range []struct {
		name string
		dst  *[]string
	}{{"Consumes", &ep.Consumes}, {"Produces", &ep.Produces}} {
		decl, ok := methods[methodKey{ep.StructName, m.name}]
		if !ok {
			if !ep.IsRaw && !ep.IsStream {
				*m.dst = []string{api.MediaJSON}
			}
			continue
		}
		if ep.IsRaw || ep.IsStream {
			return fmt.Errorf("endpoint %s: %s() is only allowed on endpoints with Handle (a raw endpoint handles content types itself, a stream answers text/event-stream)", ep.StructName, m.name)
		}
		if m.name == "Consumes" && !hasRequestBody(ep.Method) {
			return fmt.Errorf("endpoint %s: Consumes() on a %s endpoint: %s requests carry no body", ep.StructName, ep.Method, ep.Method)
		}
		types, err := parseStringSliceMethod(ep.StructName, decl)
		if err != nil {
			return err
		}
		if err := api.CheckMediaTypes(m.name, types); err != nil {
			return fmt.Errorf("endpoint %s: %w", ep.StructName, err)
		}
		*m.dst = types
	}
	return nil
}

// parseStringSliceMethod reads Consumes() or Produces(): a single return of a
// []string{...} literal of string literals.
func parseStringSliceMethod(structName string, m methodDecl) ([]string, error) {
	fname := m.fn.Name.Name
	bad := fmt.Errorf("endpoint %s: %s() must return a []string literal of string literals, e.g. `func (%s) %s() []string { return []string{\"application/x-yaml\"} }`", structName, fname, structName, fname)
	expr, ok := singleReturn(m.fn)
	lit, isLit := expr.(*ast.CompositeLit)
	if !ok || !isLit {
		return nil, bad
	}
	if arr, ok := lit.Type.(*ast.ArrayType); !ok || arr.Len != nil || !isIdent(arr.Elt, "string") {
		return nil, bad
	}
	types := []string{}
	for _, el := range lit.Elts {
		bl, ok := el.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return nil, bad
		}
		v, err := strconv.Unquote(bl.Value)
		if err != nil {
			return nil, bad
		}
		types = append(types, v)
	}
	return types, nil
}

// parseStream checks Stream(ctx context.Context, req Req) (<-chan Event, error)
// and returns the Req and Event type names.
func parseStream(structName string, m methodDecl) (reqType, eventType string, err error) {
	params, results := flatTypes(m.fn.Type.Params), flatTypes(m.fn.Type.Results)
	if len(params) == 2 && len(results) == 2 &&
		isQualified(params[0], m.file, "context", "context", "Context") &&
		isIdent(results[1], "error") {
		if ch, ok := results[0].(*ast.ChanType); ok && ch.Dir == ast.RECV {
			reqType, eventType = exprTypeName(params[1]), exprTypeName(ch.Value)
			if reqType != "" && eventType != "" {
				return reqType, eventType, nil
			}
		}
	}
	return "", "", fmt.Errorf("endpoint %s: Stream must have the signature `Stream(ctx context.Context, req Req) (<-chan Event, error)` with named Req and Event types", structName)
}

// singleReturn returns the expression of a body consisting of exactly one
// single-value return statement.
func singleReturn(fn *ast.FuncDecl) (ast.Expr, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil, false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, false
	}
	return ret.Results[0], true
}

// parseStringMethod reads Method() or Path(): it must be a single return of
// a string literal.
func parseStringMethod(structName string, m methodDecl) (string, error) {
	fname := m.fn.Name.Name
	expr, ok := singleReturn(m.fn)
	lit, isLit := expr.(*ast.BasicLit)
	if !ok || !isLit || lit.Kind != token.STRING {
		return "", fmt.Errorf("endpoint %s: %s() must return a string literal, e.g. `func (e %s) %s() string { return %s }`", structName, fname, structName, fname, map[string]string{"Method": `"GET"`, "Path": `"/api/things"`}[fname])
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", fmt.Errorf("endpoint %s: %s(): %w", structName, fname, err)
	}
	if v == "" {
		return "", fmt.Errorf("endpoint %s: %s() returns an empty string", structName, fname)
	}
	return v, nil
}

// importName returns the name under which file imports path ("." for a dot
// import), or "" if it doesn't.
func importName(file *ast.File, path, defaultName string) string {
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return defaultName
	}
	return ""
}

// isQualified reports whether expr names pkg.sel, where pkg is the file's
// name for import path (a bare sel for a dot import).
func isQualified(expr ast.Expr, file *ast.File, path, defaultName, sel string) bool {
	pkg := importName(file, path, defaultName)
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		return ok && pkg != "" && pkg != "." && x.Name == pkg && e.Sel.Name == sel
	case *ast.Ident:
		return pkg == "." && e.Name == sel
	}
	return false
}

var authLevels = map[string]api.AuthLevel{
	"AuthNone":     api.AuthNone,
	"AuthOptional": api.AuthOptional,
	"AuthRequired": api.AuthRequired,
}

// parseAuthMethod reads Auth(): it must return api.AuthLevel and be a single
// return of api.AuthNone, api.AuthOptional or api.AuthRequired (under the
// file's import name for the schemaf api package).
func parseAuthMethod(structName string, m methodDecl) (api.AuthLevel, error) {
	res := m.fn.Type.Results
	if res != nil && len(res.List) == 1 && len(res.List[0].Names) <= 1 {
		if id, ok := res.List[0].Type.(*ast.Ident); ok && id.Name == "bool" {
			return 0, fmt.Errorf("%s.Auth() returns bool; since schemaf v1.11.0 it returns api.AuthLevel: true → api.AuthRequired, false → api.AuthNone", structName)
		}
	}
	if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 ||
		!isQualified(res.List[0].Type, m.file, schemafAPIImport, "api", "AuthLevel") {
		return 0, fmt.Errorf("endpoint %s: Auth() must return api.AuthLevel (%s)", structName, schemafAPIImport)
	}
	expr, ok := singleReturn(m.fn)
	if ok {
		for constName, level := range authLevels {
			if isQualified(expr, m.file, schemafAPIImport, "api", constName) {
				return level, nil
			}
		}
	}
	return 0, fmt.Errorf("endpoint %s: Auth() must return one of api.AuthNone, api.AuthOptional, api.AuthRequired", structName)
}

// flatTypes expands a field list into one type expression per value
// (`a, b int` → int, int).
func flatTypes(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, f := range fl.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, f.Type)
		}
	}
	return out
}

func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

// parseHandle checks Handle(ctx context.Context, req Req) (Resp, error) and
// returns the Req and Resp type names.
func parseHandle(structName string, m methodDecl) (reqType, respType string, err error) {
	params, results := flatTypes(m.fn.Type.Params), flatTypes(m.fn.Type.Results)
	if len(params) == 2 && len(results) == 2 &&
		isQualified(params[0], m.file, "context", "context", "Context") &&
		isIdent(results[1], "error") {
		reqType, respType = exprTypeName(params[1]), exprTypeName(results[0])
		if reqType != "" && respType != "" {
			return reqType, respType, nil
		}
	}
	return "", "", fmt.Errorf("endpoint %s: Handle must have the signature `Handle(ctx context.Context, req Req) (Resp, error)` with named Req and Resp types", structName)
}

// checkHandleRaw checks HandleRaw(w http.ResponseWriter, r *http.Request) error.
func checkHandleRaw(structName string, m methodDecl) error {
	params, results := flatTypes(m.fn.Type.Params), flatTypes(m.fn.Type.Results)
	if len(params) == 2 && len(results) == 1 &&
		isQualified(params[0], m.file, "net/http", "http", "ResponseWriter") &&
		isIdent(results[0], "error") {
		if star, ok := params[1].(*ast.StarExpr); ok && isQualified(star.X, m.file, "net/http", "http", "Request") {
			return nil
		}
	}
	return fmt.Errorf("endpoint %s: HandleRaw must have the signature `HandleRaw(w http.ResponseWriter, r *http.Request) error`", structName)
}

// recvTypeName extracts the type name from a receiver expression.
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvTypeName(t.X)
	}
	return ""
}

// exprTypeName returns a string representation of a type expression.
func exprTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprTypeName(t.X)
	case *ast.ArrayType:
		return "[]" + exprTypeName(t.Elt)
	case *ast.SelectorExpr:
		return exprTypeName(t.X) + "." + t.Sel.Name
	}
	return ""
}

// splitDocComment splits a doc comment into summary (first line) and description (rest).
// Per Go convention, the first word of a doc comment is the symbol name — it is stripped.
func splitDocComment(comment, structName string) (summary, description string) {
	lines := strings.Split(strings.TrimSpace(comment), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", ""
	}
	first := strings.TrimSpace(lines[0])
	// Strip leading struct name (Go doc convention: "TypeName does X" → "does X")
	if strings.HasPrefix(first, structName+" ") {
		first = strings.TrimPrefix(first, structName+" ")
		// Capitalise first letter of remainder
		if len(first) > 0 {
			first = strings.ToUpper(first[:1]) + first[1:]
		}
	}
	summary = first
	if len(lines) > 1 {
		description = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	return
}

// writeEndpointsGen generates go/api/endpoints.gen.go from the template.
func writeEndpointsGen(endpoints []endpointInfo, spec []byte) error {
	src, err := renderEndpointsGen(endpoints, spec)
	if err != nil {
		return err
	}
	if err := os.WriteFile(endpointsGenOut, src, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", endpointsGenOut, err)
	}
	cli.Success("Generated %s", endpointsGenOut)
	return nil
}

// renderEndpointsGen renders the content of go/api/endpoints.gen.go, which
// registers the endpoints and the OpenAPI spec the server serves.
func renderEndpointsGen(endpoints []endpointInfo, spec []byte) ([]byte, error) {
	tmpl, err := template.New("endpoints").Parse(endpointsGenTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoints template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"Endpoints": endpoints, "Spec": string(spec)}); err != nil {
		return nil, fmt.Errorf("executing endpoints template: %w", err)
	}
	return buf.Bytes(), nil
}

// buildOpenAPISpec builds the OpenAPI spec (the content of gen/openapi.json
// and of what the server serves) from the endpoints and the component
// schemas of their types, plus schemaf's own routes and schemas.
func buildOpenAPISpec(projectName string, endpoints []endpointInfo, schemas map[string]any) ([]byte, error) {
	// The framework's /api/ routes are always part of the spec.
	paths, authSchemas := api.FrameworkOpenAPI()
	frameworkPaths, _ := api.FrameworkOpenAPI()

	for _, ep := range endpoints {
		method := strings.ToLower(ep.Method)

		op := map[string]any{
			"operationId": operationIDFromParts(ep.Method, ep.Path),
			"responses": map[string]any{
				"200":     map[string]any{"description": "OK"},
				"default": api.OpenAPIErrorResponse(),
			},
		}
		if ep.Summary != "" {
			op["summary"] = ep.Summary
		}
		if ep.Description != "" {
			op["description"] = ep.Description
		}
		if ep.Auth != api.AuthNone {
			op["security"] = api.OpenAPISecurity(ep.Auth)
		}

		if ep.ReqSchema != nil {
			op["requestBody"] = map[string]any{
				"required": true,
				"content":  mediaContent(ep.Consumes, ep.ReqSchema),
			}
		}
		if ep.RespSchema != nil {
			produces := ep.Produces
			if ep.IsStream {
				produces = []string{api.MediaEventStream}
			}
			op["responses"].(map[string]any)["200"] = map[string]any{
				"description": "OK",
				"content":     mediaContent(produces, ep.RespSchema),
			}
		}

		// Declare path parameters (e.g. {id} → parameter "id" in: path)
		if params := extractPathParams(ep.Path); len(params) > 0 {
			op["parameters"] = params
		}

		if _, ok := paths[ep.Path]; !ok {
			paths[ep.Path] = map[string]any{}
		}
		if _, taken := paths[ep.Path].(map[string]any)[method]; taken {
			if _, framework := frameworkPaths[ep.Path].(map[string]any)[method]; framework {
				return nil, fmt.Errorf("endpoint %s: %s %s is one of schemaf's own routes (see EXTEND.md, Built-in Endpoints) — choose another path", ep.StructName, ep.Method, ep.Path)
			}
			return nil, fmt.Errorf("endpoint %s: another endpoint already serves %s %s", ep.StructName, ep.Method, ep.Path)
		}
		paths[ep.Path].(map[string]any)[method] = op
	}

	allSchemas := map[string]any{}
	for name, schema := range schemas {
		allSchemas[name] = schema
	}
	for name, schema := range authSchemas {
		if _, taken := allSchemas[name]; taken {
			return nil, fmt.Errorf("type %q used by an endpoint: the OpenAPI schema name %q is reserved by schemaf — rename the type", name, name)
		}
		allSchemas[name] = schema
	}

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "API", "version": "1.0.0"},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": api.OpenAPISecuritySchemes(api.SessionCookieName(projectName)),
			"schemas":         allSchemas,
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling openapi spec: %w", err)
	}
	return data, nil
}

// writeGenFile writes a file under gen/.
func writeGenFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	cli.Success("Generated %s", path)
	return nil
}

// mediaContent lists schema under each media type.
func mediaContent(types []string, schema map[string]any) map[string]any {
	c := map[string]any{}
	for _, t := range types {
		c[t] = map[string]any{"schema": schema}
	}
	return c
}

// extractPathParams returns OpenAPI parameter objects for path template variables like {id}.
func extractPathParams(path string) []any {
	var params []any
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := segment[1 : len(segment)-1]
			params = append(params, map[string]any{
				"name":     name,
				"in":       "path",
				"required": true,
				"schema":   map[string]any{"type": "string"},
			})
		}
	}
	return params
}

func operationIDFromParts(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := strings.ToLower(method)
	for _, p := range parts {
		if p == "" {
			continue
		}
		p = strings.Trim(p, "{}")
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}
