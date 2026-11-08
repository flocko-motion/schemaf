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
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/flocko-motion/schemaf/api"
	cli "github.com/flocko-motion/schemaf/cli"
	"github.com/spf13/cobra"
)

//go:embed endpoints.gen.go.tmpl
var endpointsGenTemplate string

const (
	apiDir          = "go/api"
	endpointsGenOut = apiDir + "/endpoints.gen.go"
	openapiJSONOut  = "gen/openapi.json"
)

func newEndpointsCmd(ctx *cli.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "endpoints",
		Short: "Scan go/api/ and generate endpoints.gen.go + openapi.json",
		Long: `Scans go/api/*.go (excluding *.gen.go) for endpoint structs.

An endpoint struct is any struct that implements:
  Method() string, Path() string, Auth() api.AuthLevel,
  Handle(ctx, req Req) (Resp, error)  or  HandleRaw(w, r) error

A struct with that shape that can't be parsed is an error, never skipped.

Generates:
  go/api/endpoints.gen.go  — Provider() that registers all endpoints
  gen/openapi.json         — OpenAPI 3.0 spec (input for swagger-typescript-api)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEndpointsGen(ctx)
		},
	}
}

// endpointInfo holds everything extracted from a single endpoint struct.
type endpointInfo struct {
	StructName  string
	Method      string
	Path        string
	Auth        api.AuthLevel
	ReqType     string // empty for raw endpoints
	RespType    string // empty for raw endpoints
	Summary     string
	Description string
	IsRaw       bool
}

// fieldInfo describes one field of a Go struct for OpenAPI/TS generation.
type fieldInfo struct {
	JSONName string
	GoType   string // original Go type string
	TSType   string // TypeScript equivalent
	Required bool
}

// structInfo describes a struct type defined in the api package.
type structInfo struct {
	Name   string
	Fields []fieldInfo
}

func runEndpointsGen(_ *cli.Context) error {
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", apiDir, err)
	}

	projectName, err := readProjectName()
	if err != nil {
		return fmt.Errorf("reading project name: %w", err)
	}

	endpoints, structs, err := scanAPIDir(apiDir)
	if err != nil {
		return fmt.Errorf("scanning %s: %w", apiDir, err)
	}

	if len(endpoints) == 0 {
		cli.Warning("no endpoint structs found in %s", apiDir)
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].StructName < endpoints[j].StructName
	})

	// Generate endpoints.gen.go
	if err := writeEndpointsGen(endpoints); err != nil {
		return err
	}

	// Generate openapi.json
	if err := writeOpenAPIJSON(projectName, endpoints, structs); err != nil {
		return err
	}

	cli.Success("endpoints codegen complete: %d endpoint(s)", len(endpoints))
	return nil
}

// scanAPIDir parses all non-generated Go files in apiDir and extracts
// endpoint structs and all struct type definitions.
func scanAPIDir(dir string) ([]endpointInfo, map[string]structInfo, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		// Skip generated files
		return !strings.HasSuffix(fi.Name(), ".gen.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", dir, err)
	}

	// Collect all method declarations and struct types across files
	methods := map[methodKey]methodDecl{}
	allStructs := map[string]*ast.StructType{}
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
						st, ok := ts.Type.(*ast.StructType)
						if !ok {
							continue
						}
						allStructs[ts.Name.Name] = st
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

	names := make([]string, 0, len(allStructs))
	for name := range allStructs {
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
		return nil, nil, errors.Join(errs...)
	}

	// Build structInfo map for all structs in the package
	structs := map[string]structInfo{}
	for name, st := range allStructs {
		structs[name] = extractStructInfo(name, st)
	}

	return endpoints, structs, nil
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

	if !hasHandle && !hasHandleRaw {
		if hasMethod && hasPath && hasAuth {
			// Everything but the handler: almost certainly a misnamed Handle
			// (e.g. Handler, HandleRAW). Don't let the endpoint vanish.
			return ep, false, fmt.Errorf("endpoint %s has Method(), Path() and Auth() but no Handle or HandleRaw method: add `Handle(ctx context.Context, req Req) (Resp, error)` or `HandleRaw(w http.ResponseWriter, r *http.Request) error`", name)
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
		return ep, false, fmt.Errorf("endpoint %s is missing %s: an endpoint needs Method(), Path(), Auth() and Handle or HandleRaw", name, strings.Join(missing, ", "))
	}
	if hasHandle && hasHandleRaw {
		return ep, false, fmt.Errorf("endpoint %s has both Handle and HandleRaw — exactly one is allowed", name)
	}

	ep.StructName = name
	if ep.Method, err = parseStringMethod(name, methodFn); err != nil {
		return ep, false, err
	}
	if ep.Path, err = parseStringMethod(name, pathFn); err != nil {
		return ep, false, err
	}
	if ep.Auth, err = parseAuthMethod(name, authFn); err != nil {
		return ep, false, err
	}
	if hasHandleRaw {
		ep.IsRaw = true
		if err := checkHandleRaw(name, rawFn); err != nil {
			return ep, false, err
		}
	} else if ep.ReqType, ep.RespType, err = parseHandle(name, handleFn); err != nil {
		return ep, false, err
	}
	return ep, true, nil
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

// extractStructInfo builds a structInfo from an ast.StructType.
func extractStructInfo(name string, st *ast.StructType) structInfo {
	var fields []fieldInfo
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // embedded field, skip for now
		}
		goType := exprTypeName(field.Type)
		jsonName := ""
		if field.Tag != nil {
			tag := strings.Trim(field.Tag.Value, "`")
			jsonName = extractJSONTag(tag)
		}
		if jsonName == "" || jsonName == "-" {
			if len(field.Names) > 0 {
				jsonName = goFieldName(field.Names[0].Name)
			}
		}
		fields = append(fields, fieldInfo{
			JSONName: jsonName,
			GoType:   goType,
			TSType:   goTypeToTS(goType),
			Required: true,
		})
	}
	return structInfo{Name: name, Fields: fields}
}

// extractJSONTag pulls the name from a `json:"name,omitempty"` tag string.
func extractJSONTag(tag string) string {
	for _, part := range strings.Fields(tag) {
		if strings.HasPrefix(part, `json:"`) {
			val := strings.TrimPrefix(part, `json:"`)
			val = strings.TrimSuffix(val, `"`)
			name, _, _ := strings.Cut(val, ",")
			return name
		}
	}
	return ""
}

// goTypeToTS maps a Go type string to a TypeScript type string.
func goTypeToTS(goType string) string {
	switch goType {
	case "string":
		return "string"
	case "bool":
		return "boolean"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return "number"
	case "time.Time":
		return "string" // ISO 8601
	}
	if strings.HasPrefix(goType, "[]") {
		return goTypeToTS(goType[2:]) + "[]"
	}
	if strings.HasPrefix(goType, "*") {
		return goTypeToTS(goType[1:])
	}
	// Named struct type — reference by name
	return goType
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

// goFieldName converts a Go exported field name to a JSON-safe key.
// Handles common cases: ID→id, UserID→userId, CreatedAt→createdAt.
func goFieldName(name string) string {
	if name == "" {
		return name
	}
	// Walk runes: lowercase leading uppercase run (handles ID, URL, etc.)
	runes := []rune(name)
	i := 0
	for i < len(runes) && runes[i] >= 'A' && runes[i] <= 'Z' {
		i++
	}
	// If the whole name is uppercase (e.g. "ID"), lowercase it all
	if i == len(runes) {
		return strings.ToLower(name)
	}
	// If more than one leading uppercase and not at end, keep last uppercase as word boundary
	if i > 1 {
		i--
	}
	prefix := strings.ToLower(string(runes[:i]))
	return prefix + string(runes[i:])
}

// writeEndpointsGen generates go/api/endpoints.gen.go from the template.
func writeEndpointsGen(endpoints []endpointInfo) error {
	tmpl, err := template.New("endpoints").Parse(endpointsGenTemplate)
	if err != nil {
		return fmt.Errorf("parsing endpoints template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"Endpoints": endpoints}); err != nil {
		return fmt.Errorf("executing endpoints template: %w", err)
	}

	if err := os.WriteFile(endpointsGenOut, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", endpointsGenOut, err)
	}
	cli.Success("Generated %s", endpointsGenOut)
	return nil
}

// writeOpenAPIJSON generates openapi.json from the extracted endpoint info.
// NOTE: This is an AST-based static generator. It does not require a running server.
func writeOpenAPIJSON(projectName string, endpoints []endpointInfo, structs map[string]structInfo) error {
	// Framework auth routes (login, logout, refresh) are always part of the spec.
	paths, authSchemas := api.AuthOpenAPI()

	for _, ep := range endpoints {
		method := strings.ToLower(ep.Method)

		op := map[string]any{
			"operationId": operationIDFromParts(ep.Method, ep.Path),
			"responses": map[string]any{
				"200": map[string]any{"description": "OK"},
				"400": map[string]any{"description": "Bad Request"},
				"500": map[string]any{"description": "Internal Server Error"},
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
			op["responses"].(map[string]any)["401"] = map[string]any{"description": "Unauthorized"}
		}

		// Request body for non-GET methods with a non-empty Req type
		if !ep.IsRaw && ep.Method != "GET" && ep.Method != "DELETE" {
			if st, ok := structs[ep.ReqType]; ok && len(st.Fields) > 0 {
				op["requestBody"] = map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + ep.ReqType},
						},
					},
				}
			}
		}

		// Response schema
		if !ep.IsRaw {
			if st, ok := structs[ep.RespType]; ok && len(st.Fields) > 0 {
				op["responses"].(map[string]any)["200"] = map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/" + ep.RespType},
						},
					},
				}
			}
		}

		// Declare path parameters (e.g. {id} → parameter "id" in: path)
		if params := extractPathParams(ep.Path); len(params) > 0 {
			op["parameters"] = params
		}

		if _, ok := paths[ep.Path]; !ok {
			paths[ep.Path] = map[string]any{}
		}
		paths[ep.Path].(map[string]any)[method] = op
	}

	// Build schemas for all structs referenced
	schemas := buildSchemas(endpoints, structs)
	for name, schema := range authSchemas {
		schemas[name] = schema
	}

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "API", "version": "1.0.0"},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": api.OpenAPISecuritySchemes(api.SessionCookieName(projectName)),
			"schemas":         schemas,
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling openapi spec: %w", err)
	}

	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}
	if err := os.WriteFile(openapiJSONOut, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", openapiJSONOut, err)
	}
	cli.Success("Generated %s", openapiJSONOut)
	return nil
}

// buildSchemas collects all structs referenced by endpoints (transitively).
func buildSchemas(endpoints []endpointInfo, structs map[string]structInfo) map[string]any {
	// Collect referenced type names
	referenced := map[string]bool{}
	queue := []string{}
	for _, ep := range endpoints {
		if ep.IsRaw {
			continue
		}
		queue = append(queue, ep.ReqType, ep.RespType)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if referenced[name] {
			continue
		}
		st, ok := structs[name]
		if !ok {
			continue
		}
		referenced[name] = true
		// Follow field types that are also structs
		for _, f := range st.Fields {
			baseType := strings.TrimPrefix(strings.TrimPrefix(f.GoType, "[]"), "*")
			if _, ok := structs[baseType]; ok {
				queue = append(queue, baseType)
			}
		}
	}

	schemas := map[string]any{}
	for name := range referenced {
		st := structs[name]
		props := map[string]any{}
		var required []string
		for _, f := range st.Fields {
			baseType := strings.TrimPrefix(strings.TrimPrefix(f.GoType, "[]"), "*")
			var schema map[string]any
			if _, ok := structs[baseType]; ok {
				// Named struct → $ref
				if strings.HasPrefix(f.GoType, "[]") {
					schema = map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + baseType}}
				} else {
					schema = map[string]any{"$ref": "#/components/schemas/" + baseType}
				}
			} else {
				schema = primitiveSchema(f.GoType)
			}
			props[f.JSONName] = schema
			if f.Required {
				required = append(required, f.JSONName)
			}
		}
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		schemas[name] = s
	}
	return schemas
}

// primitiveSchema returns a JSON Schema for primitive Go types.
func primitiveSchema(goType string) map[string]any {
	goType = strings.TrimPrefix(goType, "*")
	if strings.HasPrefix(goType, "[]") {
		return map[string]any{"type": "array", "items": primitiveSchema(goType[2:])}
	}
	switch goType {
	case "string":
		return map[string]any{"type": "string"}
	case "bool":
		return map[string]any{"type": "boolean"}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return map[string]any{"type": "integer"}
	case "float32", "float64":
		return map[string]any{"type": "number"}
	case "time.Time":
		return map[string]any{"type": "string", "format": "date-time"}
	}
	return map[string]any{"type": "object"}
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
