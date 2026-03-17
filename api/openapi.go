// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
)

// openAPISchema is a minimal OpenAPI 3.0 document.
type openAPISchema struct {
	OpenAPI    string                        `json:"openapi"`
	Info       openAPIInfo                   `json:"info"`
	Paths      map[string]openAPIPathItem    `json:"paths"`
	Components openAPIComponents             `json:"components,omitempty"`
}

type openAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type openAPIComponents struct {
	SecuritySchemes map[string]openAPISecurityScheme `json:"securitySchemes,omitempty"`
}

type openAPISecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

type openAPIPathItem map[string]openAPIOperation // method → operation

type openAPIOperation struct {
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	OperationID string                     `json:"operationId,omitempty"`
	Security    []map[string][]string      `json:"security,omitempty"`
	RequestBody *openAPIRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIRequestBody struct {
	Required bool                        `json:"required"`
	Content  map[string]openAPIMediaType `json:"content"`
}

type openAPIMediaType struct {
	Schema map[string]any `json:"schema"`
}

type openAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]openAPIMediaType `json:"content,omitempty"`
}

// OpenAPIHandler returns an http.Handler that serves an OpenAPI 3.0 JSON document
// reflecting all registered routes.
func OpenAPIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc := BuildOpenAPI("API", "1.0.0")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
	})
}

// BuildOpenAPI constructs the OpenAPI spec from all registered routes.
// Exported so the codegen command can call it directly to write openapi.json.
func BuildOpenAPI(title, version string) openAPISchema {
	paths := map[string]openAPIPathItem{}

	for _, route := range Routes() {
		method := strings.ToLower(route.Method)
		path := route.Path

		op := openAPIOperation{
			Summary:     route.Summary,
			Description: route.Description,
			OperationID: operationID(route.Method, route.Path),
			Responses: map[string]openAPIResponse{
				"200": {Description: "OK"},
				"400": {Description: "Bad Request"},
				"500": {Description: "Internal Server Error"},
			},
		}

		if route.Auth {
			op.Security = []map[string][]string{{"bearerAuth": {}}}
			op.Responses["401"] = openAPIResponse{Description: "Unauthorized"}
		}

		if route.ReqType != nil {
			schema := typeToSchema(reflect.TypeOf(route.ReqType))
			op.RequestBody = &openAPIRequestBody{
				Required: true,
				Content: map[string]openAPIMediaType{
					"application/json": {Schema: schema},
				},
			}
		}

		if route.RespType != nil {
			schema := typeToSchema(reflect.TypeOf(route.RespType))
			op.Responses["200"] = openAPIResponse{
				Description: "OK",
				Content: map[string]openAPIMediaType{
					"application/json": {Schema: schema},
				},
			}
		}

		if _, ok := paths[path]; !ok {
			paths[path] = openAPIPathItem{}
		}
		paths[path][method] = op
	}

	return openAPISchema{
		OpenAPI: "3.0.0",
		Info:    openAPIInfo{Title: title, Version: version},
		Paths:   paths,
		Components: openAPIComponents{
			SecuritySchemes: map[string]openAPISecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
				},
			},
		},
	}
}

// operationID generates a camelCase operation ID from method + path.
func operationID(method, path string) string {
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

// typeToSchema converts a Go type (via reflection) into a simple JSON Schema map.
func typeToSchema(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := fieldName(f)
			props[name] = typeToSchema(f.Type)
			required = append(required, name)
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	case reflect.Slice:
		return map[string]any{"type": "array", "items": typeToSchema(t.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{"type": "object"}
	}
}

// fieldName returns the JSON key for a struct field (json tag or lower-cased name).
func fieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return strings.ToLower(f.Name)
	}
	return name
}
