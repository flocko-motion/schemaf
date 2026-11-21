// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
)

// paramTagged reports whether a struct field's tag carries a path or query
// tag: such a field is a parameter, never part of the request body.
func paramTagged(tag string) bool {
	st := reflect.StructTag(tag)
	return st.Get("path") != "" || st.Get("query") != ""
}

// pathTemplateParams returns the {name} segments of path, in order.
func pathTemplateParams(path string) []string {
	var names []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			names = append(names, segment[1:len(segment)-1])
		}
	}
	return names
}

// scalarParamSchema returns the schema of a string, integer or bool type
// (through aliases and named types), or nil for any other type.
func scalarParamSchema(t types.Type) map[string]any {
	bt, ok := types.Unalias(t).Underlying().(*types.Basic)
	if !ok {
		return nil
	}
	switch {
	case bt.Info()&types.IsString != 0:
		return map[string]any{"type": "string"}
	case bt.Info()&types.IsInteger != 0 && bt.Kind() != types.Uintptr:
		return map[string]any{"type": "integer"}
	case bt.Info()&types.IsBoolean != 0:
		return map[string]any{"type": "boolean"}
	}
	return nil
}

// params checks the path and query fields of endpoint ep's request type req
// against ep.Path and returns the OpenAPI parameters: the path params in
// the order of the template, then the query params in field order. See
// EXTEND.md, "Path and query parameters". Errors are recorded on b.
func (b *schemaBuilder) params(ep *endpointInfo, req types.Type) []any {
	where := fmt.Sprintf("endpoint %s: request type %s", ep.StructName, typeName(req))
	pathFields := map[string]map[string]any{} // path param name → parameter
	var query []any
	if st := structOf(req); st != nil {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			tag := reflect.StructTag(st.Tag(i))
			pathName, queryName := tag.Get("path"), tag.Get("query")
			fieldWhere := fmt.Sprintf("%s, field %s", where, f.Name())
			if pathName == "" && queryName == "" {
				if f.Embedded() {
					if inner := structOf(f.Type()); inner != nil && hasParamTags(inner, map[*types.Struct]bool{}) {
						b.errorf("%s: embedded struct %s has `path` or `query` tagged fields — schemaf reads parameters only from the request type's own fields; declare them there", fieldWhere, typeName(f.Type()))
						continue
					}
				}
				if f.Exported() && !hasRequestBody(ep.Method) {
					b.errorf("%s: a %s request has no body, so this field can never be filled — tag it `path:\"…\"` or `query:\"…\"`, or remove it", fieldWhere, ep.Method)
				}
				continue
			}
			if !f.Exported() {
				b.errorf("%s: the field is unexported, so it can't be filled — export it", fieldWhere)
				continue
			}
			if pathName != "" && queryName != "" {
				b.errorf("%s: tagged both `path` and `query` — a field takes its value from one place; keep one tag", fieldWhere)
				continue
			}
			if tag.Get("json") != "" {
				kind := "query"
				if pathName != "" {
					kind = "path"
				}
				b.errorf("%s: tagged both `json` and `%s` — path and query fields are not part of the body; remove the json tag", fieldWhere, kind)
				continue
			}
			if pathName != "" {
				s := scalarParamSchema(f.Type())
				if s == nil {
					b.errorf("%s: path field of type %s — a path field must be a string, an integer type or bool (a path segment is always there and holds one value, so no pointers or slices)", fieldWhere, f.Type())
					continue
				}
				if _, dup := pathFields[pathName]; dup {
					b.errorf("%s: another field is already tagged `path:%q` — tag one field per path param", fieldWhere, pathName)
					continue
				}
				pathFields[pathName] = map[string]any{"name": pathName, "in": "path", "required": true, "schema": s}
				continue
			}
			p := queryParam(f.Type(), queryName)
			if p == nil {
				b.errorf("%s: query field of type %s — a query field must be a string, an integer type or bool, a pointer to one (optional) or a slice of one (repeated)", fieldWhere, f.Type())
				continue
			}
			query = append(query, p)
		}
	}

	var out []any
	inTemplate := map[string]bool{}
	for _, name := range pathTemplateParams(ep.Path) {
		inTemplate[name] = true
		p, ok := pathFields[name]
		if !ok {
			b.errorf("%s: Path() %q has {%s}, but no field is tagged `path:%q` — add a field with that tag", where, ep.Path, name, name)
			continue
		}
		out = append(out, p)
	}
	for name := range pathFields {
		if !inTemplate[name] {
			b.errorf("%s: a field is tagged `path:%q`, but Path() %q has no {%s} — fix the tag or the path", where, name, ep.Path, name)
		}
	}
	return append(out, query...)
}

// queryParam returns the parameter of a query field of type t, or nil if
// the type isn't supported.
func queryParam(t types.Type, name string) map[string]any {
	p := map[string]any{"name": name, "in": "query"}
	switch tt := types.Unalias(t).Underlying().(type) {
	case *types.Pointer:
		s := scalarParamSchema(tt.Elem())
		if s == nil {
			return nil
		}
		p["required"], p["schema"] = false, s
		return p
	case *types.Slice:
		s := scalarParamSchema(tt.Elem())
		if s == nil {
			return nil
		}
		p["required"], p["style"], p["explode"] = false, "form", true
		p["schema"] = map[string]any{"type": "array", "items": s}
		return p
	}
	s := scalarParamSchema(t)
	if s == nil {
		return nil
	}
	p["required"], p["schema"] = true, s
	return p
}

// hasParamTags reports whether st, or a struct embedded in it, has a field
// tagged `path` or `query`.
func hasParamTags(st *types.Struct, visiting map[*types.Struct]bool) bool {
	if visiting[st] {
		return false
	}
	visiting[st] = true
	for i := 0; i < st.NumFields(); i++ {
		if paramTagged(st.Tag(i)) {
			return true
		}
		if f := st.Field(i); f.Embedded() {
			if inner := structOf(f.Type()); inner != nil && hasParamTags(inner, visiting) {
				return true
			}
		}
	}
	return false
}

// typeName returns t's name without the package path, e.g. "GetUserReq".
func typeName(t types.Type) string {
	return types.TypeString(t, func(*types.Package) string { return "" })
}
