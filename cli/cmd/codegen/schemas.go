// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

const schemaRefPrefix = "#/components/schemas/"

// loadSchemas type-checks the package in dir (with every package it
// imports) and builds the OpenAPI schemas of the typed endpoints' request
// and response types, wherever those types are declared: in dir, in another
// package of the module or in a dependency, directly or through an alias.
//
// It sets ReqSchema (only for endpoints whose request has a JSON body) and
// RespSchema on each typed endpoint, and returns the named component schemas
// they reference. A package that doesn't type-check, a type without a JSON
// shape and two types sharing a schema name are errors.
func loadSchemas(dir string, endpoints []endpointInfo) (map[string]any, error) {
	b := &schemaBuilder{components: map[string]any{}, owners: map[string]*types.TypeName{}}
	hasTyped := slices.ContainsFunc(endpoints, func(ep endpointInfo) bool { return !ep.IsRaw })
	if !hasTyped {
		return b.components, nil
	}
	pkg, err := loadAPIPackage(dir)
	if err != nil {
		return nil, err
	}
	for i := range endpoints {
		ep := &endpoints[i]
		if ep.IsRaw {
			continue
		}
		req, resp, err := handleTypes(pkg, ep.StructName)
		if err != nil {
			return nil, err
		}
		if ep.Method != "GET" && ep.Method != "DELETE" && b.hasJSONFields(req) {
			ep.ReqSchema = b.schema(req, ep.StructName+" request")
		}
		if !b.isEmptyStruct(resp) {
			ep.RespSchema = b.schema(resp, ep.StructName+" response")
		}
	}
	if len(b.errs) > 0 {
		return nil, errors.Join(b.errs...)
	}
	return b.components, nil
}

// loadAPIPackage type-checks the package in dir. Generated files (*.gen.go)
// are replaced by their bare package clause: codegen is about to rewrite
// them, and stale output must not break type checking.
func loadAPIPackage(dir string) (*types.Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	overlay := map[string][]byte{}
	gens, _ := filepath.Glob(filepath.Join(abs, "*.gen.go"))
	for _, g := range gens {
		f, err := parser.ParseFile(token.NewFileSet(), g, nil, parser.PackageClauseOnly)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", g, err)
		}
		overlay[g] = []byte("package " + f.Name.Name + "\n")
	}
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir:     abs,
		Overlay: overlay,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", dir, err)
	}
	var errs []error
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, errors.New(e.Error()))
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("type-checking %s for the OpenAPI schemas: %w", dir, errors.Join(errs...))
	}
	if len(pkgs) != 1 || pkgs[0].Types == nil {
		return nil, fmt.Errorf("loading %s: expected one package, got %d", dir, len(pkgs))
	}
	return pkgs[0].Types, nil
}

// handleTypes returns the Req and Resp types of endpoint structName's
// Handle(ctx, req Req) (Resp, error).
func handleTypes(pkg *types.Package, structName string) (req, resp types.Type, err error) {
	obj := pkg.Scope().Lookup(structName)
	if obj == nil {
		return nil, nil, fmt.Errorf("endpoint %s: type not found in %s", structName, pkg.Path())
	}
	m, _, _ := types.LookupFieldOrMethod(obj.Type(), true, pkg, "Handle")
	fn, ok := m.(*types.Func)
	if !ok {
		return nil, nil, fmt.Errorf("endpoint %s: no Handle method", structName)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 2 || sig.Results().Len() != 2 {
		return nil, nil, fmt.Errorf("endpoint %s: Handle must have the signature `Handle(ctx context.Context, req Req) (Resp, error)`", structName)
	}
	return sig.Params().At(1).Type(), sig.Results().At(0).Type(), nil
}

// schemaBuilder turns Go types into OpenAPI schemas following encoding/json.
type schemaBuilder struct {
	components map[string]any
	owners     map[string]*types.TypeName // schema name → the type it was built from
	errs       []error
}

func (b *schemaBuilder) errorf(format string, args ...any) {
	b.errs = append(b.errs, fmt.Errorf(format, args...))
}

// structOf returns the struct type underlying t (through aliases and one
// pointer), or nil.
func structOf(t types.Type) *types.Struct {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if isSpecialNamed(t) {
		return nil
	}
	st, _ := t.Underlying().(*types.Struct)
	return st
}

func (b *schemaBuilder) hasJSONFields(t types.Type) bool {
	st := structOf(t)
	return st != nil && len(jsonFields(st)) > 0
}

func (b *schemaBuilder) isEmptyStruct(t types.Type) bool {
	st := structOf(t)
	return st != nil && len(jsonFields(st)) == 0
}

// namedPath returns "pkgpath.Name" for a named type, or "".
func namedPath(t types.Type) string {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return ""
	}
	return n.Obj().Pkg().Path() + "." + n.Obj().Name()
}

// specialSchemas are named types whose JSON form is not their Go shape.
var specialSchemas = map[string]map[string]any{
	"time.Time":                       {"type": "string", "format": "date-time"},
	"github.com/google/uuid.UUID":     {"type": "string", "format": "uuid"},
	"encoding/json.RawMessage":        {},
	"github.com/google/uuid.NullUUID": {"type": "string", "format": "uuid", "nullable": true},
}

func isSpecialNamed(t types.Type) bool {
	_, ok := specialSchemas[namedPath(t)]
	return ok
}

// schema returns the schema of t. where names the place t is used, for
// error messages.
func (b *schemaBuilder) schema(t types.Type, where string) map[string]any {
	t = types.Unalias(t)
	if s, ok := specialSchemas[namedPath(t)]; ok {
		return copySchema(s)
	}
	if n, ok := t.(*types.Named); ok {
		switch {
		case hasMethod(n, "MarshalJSON"):
			b.errorf("%s: %s implements json.Marshaler, so its JSON shape is unknown to codegen — use a plain type in the API", where, qualifiedName(n.Obj()))
			return map[string]any{}
		case hasMethod(n, "MarshalText"):
			return map[string]any{"type": "string"}
		}
	}
	switch tt := t.(type) {
	case *types.Pointer:
		return b.schema(tt.Elem(), where)
	case *types.Named:
		if _, isStruct := tt.Underlying().(*types.Struct); isStruct {
			return b.ref(tt, where)
		}
		return b.schema(tt.Underlying(), where)
	case *types.Basic:
		switch {
		case tt.Info()&types.IsBoolean != 0:
			return map[string]any{"type": "boolean"}
		case tt.Info()&types.IsInteger != 0:
			return map[string]any{"type": "integer"}
		case tt.Info()&types.IsFloat != 0:
			return map[string]any{"type": "number"}
		case tt.Info()&types.IsString != 0:
			return map[string]any{"type": "string"}
		}
	case *types.Slice:
		if isByte(tt.Elem()) {
			return map[string]any{"type": "string", "format": "byte"} // base64
		}
		return map[string]any{"type": "array", "items": b.schema(tt.Elem(), where)}
	case *types.Array:
		return map[string]any{"type": "array", "items": b.schema(tt.Elem(), where)}
	case *types.Map:
		return map[string]any{"type": "object", "additionalProperties": b.schema(tt.Elem(), where)}
	case *types.Struct:
		return b.object(tt, where)
	case *types.Interface:
		return map[string]any{} // any JSON value
	}
	b.errorf("%s: type %s has no JSON schema — use a JSON-encodable type or tag the field `json:\"-\"`", where, t)
	return map[string]any{}
}

// hasMethod reports whether n or *n has method name.
func hasMethod(n *types.Named, name string) bool {
	ms := types.NewMethodSet(types.NewPointer(n))
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Obj().Name() == name {
			return true
		}
	}
	return false
}

func isByte(t types.Type) bool {
	bt, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && bt.Kind() == types.Byte
}

func copySchema(s map[string]any) map[string]any {
	out := make(map[string]any, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// ref returns a $ref to the component schema of named struct type n,
// building it on first use. The schema name is the type's name; two types
// with the same name are an error naming both.
func (b *schemaBuilder) ref(n *types.Named, where string) map[string]any {
	obj := n.Origin().Obj()
	name := obj.Name()
	if n.TypeArgs().Len() > 0 {
		b.errorf("%s: generic type %s has no OpenAPI schema name — declare a non-generic type", where, n)
		return map[string]any{}
	}
	if owner, ok := b.owners[name]; ok {
		if owner != obj {
			b.errorf("OpenAPI schema name %q is used by both %s and %s — rename one of the types", name, qualifiedName(owner), qualifiedName(obj))
		}
		return map[string]any{"$ref": schemaRefPrefix + name}
	}
	b.owners[name] = obj
	b.components[name] = nil // placeholder: recursive types refer to it
	b.components[name] = b.object(n.Underlying().(*types.Struct), name)
	return map[string]any{"$ref": schemaRefPrefix + name}
}

func qualifiedName(obj *types.TypeName) string {
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

// object returns the object schema of a struct: its JSON fields (embedded
// structs flattened), each required unless it has omitempty/omitzero or is
// a pointer.
func (b *schemaBuilder) object(st *types.Struct, where string) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range jsonFields(st) {
		s := b.schema(f.typ, where+"."+f.goName)
		if f.asString {
			s = map[string]any{"type": "string"}
		}
		props[f.name] = s
		if !f.optional {
			required = append(required, f.name)
		}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// jsonField is one field of a struct's JSON encoding.
type jsonField struct {
	name     string // JSON key
	goName   string
	typ      types.Type
	optional bool
	asString bool // `json:",string"` on a number or bool
	depth    int  // embedding depth
	tagged   bool // name comes from the json tag
}

// jsonFields lists the fields encoding/json encodes for st, in order:
// exported fields, renamed by their json tag, without `json:"-"`; fields of
// untagged embedded structs promoted, and conflicts resolved as
// encoding/json does (the shallowest field wins; among equally deep ones a
// tagged field wins; otherwise all of them are dropped).
func jsonFields(st *types.Struct) []jsonField {
	all := collectFields(st, 0, map[*types.Struct]bool{})
	byName := map[string][]jsonField{}
	var order []string
	for _, f := range all {
		if _, seen := byName[f.name]; !seen {
			order = append(order, f.name)
		}
		byName[f.name] = append(byName[f.name], f)
	}
	var out []jsonField
	for _, name := range order {
		if f, ok := dominantField(byName[name]); ok {
			out = append(out, f)
		}
	}
	return out
}

func dominantField(fs []jsonField) (jsonField, bool) {
	minDepth := fs[0].depth
	for _, f := range fs {
		minDepth = min(minDepth, f.depth)
	}
	var top []jsonField
	for _, f := range fs {
		if f.depth == minDepth {
			top = append(top, f)
		}
	}
	if len(top) == 1 {
		return top[0], true
	}
	var tagged []jsonField
	for _, f := range top {
		if f.tagged {
			tagged = append(tagged, f)
		}
	}
	if len(tagged) == 1 {
		return tagged[0], true
	}
	return jsonField{}, false
}

func collectFields(st *types.Struct, depth int, visiting map[*types.Struct]bool) []jsonField {
	if visiting[st] {
		return nil
	}
	visiting[st] = true
	defer delete(visiting, st)
	var out []jsonField
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i)).Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		optList := strings.Split(opts, ",")
		if f.Embedded() && name == "" {
			if inner := structOf(f.Type()); inner != nil {
				out = append(out, collectFields(inner, depth+1, visiting)...)
				continue
			}
			if !f.Exported() {
				continue
			}
		}
		if !f.Exported() {
			continue
		}
		jf := jsonField{name: name, goName: f.Name(), typ: f.Type(), depth: depth, tagged: name != ""}
		if name == "" {
			jf.name = f.Name()
		}
		_, isPtr := types.Unalias(f.Type()).(*types.Pointer)
		jf.optional = isPtr || slices.Contains(optList, "omitempty") || slices.Contains(optList, "omitzero")
		if slices.Contains(optList, "string") {
			if bt, ok := types.Unalias(f.Type()).Underlying().(*types.Basic); ok && bt.Info()&(types.IsNumeric|types.IsBoolean) != 0 {
				jf.asString = true
			}
		}
		out = append(out, jf)
	}
	return out
}
