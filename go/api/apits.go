package api

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
)

// APITSHandler returns an http.Handler that generates a TypeScript client
// from all registered routes. Served at /openapi.ts.
func APITSHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := buildAPITS()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, ts)
	})
}

func buildAPITS() string {
	var sb strings.Builder

	sb.WriteString("// Auto-generated TypeScript API client. Do not edit manually.\n\n")

	for _, route := range Routes() {
		fnName := tsFunctionName(route.Method, route.Path, route.Summary)
		reqParam := ""
		reqArg := ""
		if route.ReqType != nil {
			t := reflect.TypeOf(route.ReqType)
			for t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			tsType := goTypeToTS(t)
			reqParam = fmt.Sprintf("body: %s, ", tsType)
			reqArg = `, body: JSON.stringify(body), headers: { "Content-Type": "application/json" }`
		}

		respType := "unknown"
		if route.RespType != nil {
			t := reflect.TypeOf(route.RespType)
			for t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			respType = goTypeToTS(t)
		}

		// Build URL with path params
		urlExpr := buildURLExpr(route.Path)

		sb.WriteString(fmt.Sprintf("// %s\n", route.Summary))
		sb.WriteString(fmt.Sprintf("export async function %s(%surl = %q): Promise<%s> {\n",
			fnName, reqParam, urlExpr, respType))
		sb.WriteString(fmt.Sprintf("  const res = await fetch(url, { method: %q%s });\n",
			route.Method, reqArg))
		sb.WriteString("  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);\n")
		sb.WriteString(fmt.Sprintf("  return res.json() as Promise<%s>;\n", respType))
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

// tsFunctionName derives a camelCase function name from method, path, and summary.
func tsFunctionName(method, path, summary string) string {
	if summary != "" {
		// camelCase from summary words
		words := strings.Fields(summary)
		if len(words) > 0 {
			result := strings.ToLower(words[0])
			for _, w := range words[1:] {
				if len(w) > 0 {
					result += strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
				}
			}
			return result
		}
	}
	return operationID(method, path)
}

// buildURLExpr returns a TS default URL string for a given path, replacing {param} with ${param}.
func buildURLExpr(path string) string {
	if !strings.Contains(path, "{") {
		return `"` + path + `"`
	}
	// Replace Go path params {param} with JS template literal params ${param}.
	// Process left-to-right, tracking write position to avoid revisiting replacements.
	var sb strings.Builder
	remaining := path
	for {
		start := strings.Index(remaining, "{")
		if start < 0 {
			sb.WriteString(remaining)
			break
		}
		end := strings.Index(remaining[start:], "}")
		if end < 0 {
			sb.WriteString(remaining)
			break
		}
		end += start // absolute index
		param := remaining[start+1 : end]
		sb.WriteString(remaining[:start])
		sb.WriteString("${")
		sb.WriteString(param)
		sb.WriteString("}")
		remaining = remaining[end+1:]
	}
	return "`" + sb.String() + "`"
}

// goTypeToTS converts a Go reflect.Type to a TypeScript type string.
func goTypeToTS(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		var fields []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := fieldName(f)
			fields = append(fields, fmt.Sprintf("  %s: %s", name, goTypeToTS(f.Type)))
		}
		if len(fields) == 0 {
			return "Record<string, unknown>"
		}
		return "{\n" + strings.Join(fields, ";\n") + "\n}"
	case reflect.Slice:
		return goTypeToTS(t.Elem()) + "[]"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "number"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "unknown"
	}
}

// capitalize returns s with its first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// ensure capitalize is used (avoids unused import lint)
var _ = capitalize
