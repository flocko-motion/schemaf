// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// NOTE: UNTESTED draft — new code, not yet exercised end-to-end.

package codegen

import (
	"bytes"
	"encoding/json"
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	cli "github.com/flocko-motion/schemaf/cli"
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
  Method() string, Path() string, Auth() bool,
  Handle(ctx, req Req) (Resp, error)

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
	Auth        bool
	ReqType     string
	RespType    string
	Summary     string
	Description string
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
	if err := writeOpenAPIJSON(endpoints, structs); err != nil {
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
	type methodKey struct{ recv, name string }
	methods := map[methodKey]*ast.FuncDecl{}
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
					methods[methodKey{recv, d.Name.Name}] = d
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

	// Identify endpoint structs: must have Method, Path, Auth, Handle methods
	var endpoints []endpointInfo
	for name := range allStructs {
		methodFn, hasMethod := methods[methodKey{name, "Method"}]
		pathFn, hasPath := methods[methodKey{name, "Path"}]
		authFn, hasAuth := methods[methodKey{name, "Auth"}]
		handleFn, hasHandle := methods[methodKey{name, "Handle"}]
		if !hasMethod || !hasPath || !hasAuth || !hasHandle {
			continue
		}

		httpMethod, ok := extractStringReturn(methodFn.Body)
		if !ok {
			continue
		}
		routePath, ok := extractStringReturn(pathFn.Body)
		if !ok {
			continue
		}
		authRequired, ok := extractBoolReturn(authFn.Body)
		if !ok {
			continue
		}
		reqType, respType, ok := extractHandleTypes(handleFn.Type)
		if !ok {
			continue
		}

		summary, description := splitDocComment(docComments[name], name)

		endpoints = append(endpoints, endpointInfo{
			StructName:  name,
			Method:      httpMethod,
			Path:        routePath,
			Auth:        authRequired,
			ReqType:     reqType,
			RespType:    respType,
			Summary:     summary,
			Description: description,
		})
	}

	// Build structInfo map for all structs in the package
	structs := map[string]structInfo{}
	for name, st := range allStructs {
		structs[name] = extractStructInfo(name, st)
	}

	return endpoints, structs, nil
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

// extractStringReturn finds the string literal in a single-return function body.
func extractStringReturn(body *ast.BlockStmt) (string, bool) {
	if body == nil || len(body.List) == 0 {
		return "", false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return "", false
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	// Strip surrounding quotes
	s := lit.Value
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], true
	}
	return s, true
}

// extractBoolReturn finds the bool literal in a single-return function body.
func extractBoolReturn(body *ast.BlockStmt) (bool, bool) {
	if body == nil || len(body.List) == 0 {
		return false, false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false, false
	}
	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return false, false
	}
	switch ident.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// extractHandleTypes gets the Req and Resp type names from Handle(ctx, req Req) (Resp, error).
func extractHandleTypes(ft *ast.FuncType) (reqType, respType string, ok bool) {
	if ft.Params == nil || len(ft.Params.List) < 2 {
		return "", "", false
	}
	if ft.Results == nil || len(ft.Results.List) < 2 {
		return "", "", false
	}
	// Second param is Req
	reqType = exprTypeName(ft.Params.List[1].Type)
	// First result is Resp
	respType = exprTypeName(ft.Results.List[0].Type)
	if reqType == "" || respType == "" {
		return "", "", false
	}
	return reqType, respType, true
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
func writeOpenAPIJSON(endpoints []endpointInfo, structs map[string]structInfo) error {
	paths := map[string]any{}

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
		if ep.Auth {
			op["security"] = []any{map[string]any{"bearerAuth": []any{}}}
			op["responses"].(map[string]any)["401"] = map[string]any{"description": "Unauthorized"}
		}

		// Request body for non-GET methods with a non-empty Req type
		if ep.Method != "GET" && ep.Method != "DELETE" {
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

		if _, ok := paths[ep.Path]; !ok {
			paths[ep.Path] = map[string]any{}
		}
		paths[ep.Path].(map[string]any)[method] = op
	}

	// Build schemas for all structs referenced
	schemas := buildSchemas(endpoints, structs)

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "API", "version": "1.0.0"},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
			"schemas": schemas,
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
