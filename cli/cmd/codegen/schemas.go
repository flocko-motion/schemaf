// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
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
// It sets ReqSchema (only for endpoints whose request has a JSON body),
// Params (the path and query parameters) and RespSchema on each typed
// endpoint, and returns the named component schemas
// they reference, and the schema sources: one glob per project package
// outside dir that declares a type the schemas are built from, plus
// go/go.mod when one comes from a dependency (see gen/schema-sources). A
// package that doesn't type-check, a type without a JSON shape and two
// types sharing a schema name are errors.
func loadSchemas(dir string, endpoints []endpointInfo) (components map[string]any, sources []string, err error) {
	b := &schemaBuilder{components: map[string]any{}, owners: map[string]*types.TypeName{}, used: map[*types.Package]bool{}}
	hasTyped := slices.ContainsFunc(endpoints, func(ep endpointInfo) bool { return !ep.IsRaw })
	if !hasTyped {
		return b.components, nil, nil
	}
	pkg, err := loadAPIPackage(dir)
	if err != nil {
		return nil, nil, err
	}
	for i := range endpoints {
		ep := &endpoints[i]
		if ep.IsRaw {
			continue
		}
		req, resp, err := handleTypes(pkg.Types, ep.StructName, ep.IsStream)
		if err != nil {
			return nil, nil, err
		}
		// Non-nil even when empty: a nil Params makes buildOpenAPISpec fall
		// back to the untyped path params of Path(), as for raw endpoints.
		ep.Params = append([]any{}, b.params(ep, req)...)
		if hasRequestBody(ep.Method) {
			if structOf(req) == nil {
				b.errorf("endpoint %s: %s request type %s is not a struct — a request body is a JSON object", ep.StructName, ep.Method, req)
			} else if b.fieldCount(req) > 0 {
				b.requestRoot = true
				ep.ReqSchema = b.schema(req, ep.StructName+" request")
				b.requestRoot = false
			}
		}
		// Checked before the field count: a response of only path/query
		// fields counts zero fields and would get no schema at all.
		if b.checkParamStruct(derefPointer(resp), ep.StructName+" response") {
			continue
		}
		if structOf(resp) == nil || b.fieldCount(resp) > 0 {
			ep.RespSchema = b.schema(resp, ep.StructName+" response")
		}
	}
	if len(b.errs) > 0 {
		return nil, nil, errors.Join(b.errs...)
	}
	sources, err = schemaSources(pkg, b.used)
	if err != nil {
		return nil, nil, err
	}
	return b.components, sources, nil
}

// hasRequestBody reports whether requests with method carry a JSON body.
func hasRequestBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	}
	return false
}

// schemaSources maps the packages the schemas were built from to the globs
// of gen/schema-sources: "<dir>/*.go" for each package of the project's
// module other than the api package itself, "go/go.mod" if any package comes
// from a dependency. Standard library packages are left out. Paths are
// relative to the project root (the working directory).
func schemaSources(api *packages.Package, used map[*types.Package]bool) ([]string, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var out []string
	var errs []error
	packages.Visit([]*packages.Package{api}, nil, func(p *packages.Package) {
		if p == api || !used[p.Types] || p.Module == nil {
			return // the api package is an input already; nil Module: std
		}
		if !p.Module.Main {
			out = append(out, "go/go.mod")
			return
		}
		if len(p.GoFiles) == 0 {
			return
		}
		rel, err := filepath.Rel(root, filepath.Dir(p.GoFiles[0]))
		if err != nil || strings.HasPrefix(rel, "..") {
			errs = append(errs, fmt.Errorf("package %s is outside the project root %s", p.PkgPath, root))
			return
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsAny(rel, " \t\n*?[]|\\") {
			errs = append(errs, fmt.Errorf("package directory %q contains whitespace or glob characters — it can't be a codegen input; rename it", rel))
			return
		}
		out = append(out, rel+"/*.go")
	})
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// loadAPIPackage type-checks the package in dir. Generated files (*.gen.go)
// are replaced by their bare package clause: codegen is about to rewrite
// them, and stale output must not break type checking.
func loadAPIPackage(dir string) (*packages.Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	overlay := map[string][]byte{}
	gens, err := filepath.Glob(filepath.Join(abs, "*.gen.go"))
	if err != nil {
		return nil, err
	}
	for _, g := range gens {
		f, err := parser.ParseFile(token.NewFileSet(), g, nil, parser.PackageClauseOnly)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", g, err)
		}
		overlay[g] = []byte("package " + f.Name.Name + "\n")
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
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
	return pkgs[0], nil
}

// handleTypes returns the Req and Resp types of endpoint structName's
// Handle(ctx, req Req) (Resp, error), or the Req and Event types of its
// Stream(ctx, req Req) (<-chan Event, error).
func handleTypes(pkg *types.Package, structName string, isStream bool) (req, resp types.Type, err error) {
	obj := pkg.Scope().Lookup(structName)
	if obj == nil {
		return nil, nil, fmt.Errorf("endpoint %s: type not found in %s", structName, pkg.Path())
	}
	name, shape := "Handle", "`Handle(ctx context.Context, req Req) (Resp, error)`"
	if isStream {
		name, shape = "Stream", "`Stream(ctx context.Context, req Req) (<-chan Event, error)`"
	}
	m, _, _ := types.LookupFieldOrMethod(obj.Type(), true, pkg, name)
	fn, ok := m.(*types.Func)
	if !ok {
		return nil, nil, fmt.Errorf("endpoint %s: no %s method", structName, name)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 2 || sig.Results().Len() != 2 {
		return nil, nil, fmt.Errorf("endpoint %s: %s must have the signature %s", structName, name, shape)
	}
	resp = sig.Results().At(0).Type()
	if isStream {
		// The schema of a stream is the schema of each event: the channel's element.
		ch, ok := types.Unalias(resp).Underlying().(*types.Chan)
		if !ok {
			return nil, nil, fmt.Errorf("endpoint %s: %s must have the signature %s", structName, name, shape)
		}
		resp = ch.Elem()
	}
	return sig.Params().At(1).Type(), resp, nil
}

// schemaBuilder turns Go types into OpenAPI schemas following encoding/json.
type schemaBuilder struct {
	components map[string]any
	owners     map[string]*types.TypeName // schema name → the type it was built from
	used       map[*types.Package]bool    // packages declaring a type a schema depends on
	errs       []error
	// requestRoot is set while building an endpoint's request schema, until
	// the request struct itself is reached: the one place a struct with
	// path/query fields may appear (see checkParamStruct).
	requestRoot bool
}

// note records that a schema depends on the declaration of named type n.
func (b *schemaBuilder) note(n *types.Named) {
	if pkg := n.Obj().Pkg(); pkg != nil {
		b.used[pkg] = true
	}
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

// fieldCount returns the number of JSON fields of the struct underlying t
// (see structOf).
func (b *schemaBuilder) fieldCount(t types.Type) int {
	return len(b.jsonFields(structOf(t)))
}

// namedPath returns "pkgpath.Name" for a named type, or "".
func namedPath(t types.Type) string {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return ""
	}
	return qualifiedName(n.Obj())
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
	if b.checkParamStruct(t, where) {
		return map[string]any{}
	}
	if n, ok := t.(*types.Named); ok {
		b.note(n)
		switch {
		case hasMethod(n, "MarshalJSON"):
			b.errorf("%s: %s implements json.Marshaler, so its JSON shape is unknown to codegen — if it encodes as a JSON string, implement encoding.TextMarshaler instead; otherwise use a plain struct in the API", where, qualifiedName(n.Obj()))
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

// derefPointer returns the element of a pointer type (through aliases), or t.
func derefPointer(t types.Type) types.Type {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		return types.Unalias(p.Elem())
	}
	return t
}

// checkParamStruct records an error if t is a struct with path or query
// fields anywhere but as an endpoint's request type. Those fields are left
// out of its schema, so in a response, an event or another type's field the
// schema would hide fields the JSON carries.
// It reports whether it recorded one.
func (b *schemaBuilder) checkParamStruct(t types.Type, where string) bool {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	root := b.requestRoot
	b.requestRoot = false // only the request struct itself, not what it contains
	if root || !hasParamTags(st, map[*types.Struct]bool{}) {
		return false
	}
	b.errorf("%s: type %s has path or query fields, so it is a request type and can't be used here — declare a separate type", where, typeName(t))
	return true
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
// structs flattened), each required unless it has omitempty/omitzero, is a
// pointer or is promoted through an embedded pointer (all left out when it
// is nil).
func (b *schemaBuilder) object(st *types.Struct, where string) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range b.jsonFields(st) {
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
// tagged field wins; otherwise all of them are dropped). A nil st has none.
func (b *schemaBuilder) jsonFields(st *types.Struct) []jsonField {
	if st == nil {
		return nil
	}
	all := b.collectFields(st, 0, false, map[*types.Struct]bool{})
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

// collectFields lists st's JSON fields before conflict resolution. viaPtr
// is true below an embedded pointer: those fields are all optional.
func (b *schemaBuilder) collectFields(st *types.Struct, depth int, viaPtr bool, visiting map[*types.Struct]bool) []jsonField {
	if visiting[st] {
		return nil
	}
	visiting[st] = true
	defer delete(visiting, st)
	var out []jsonField
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i)).Get("json")
		if tag == "-" || paramTagged(st.Tag(i)) {
			continue // path and query fields are parameters, not body
		}
		name, opts, _ := strings.Cut(tag, ",")
		optList := strings.Split(opts, ",")
		if f.Embedded() && name == "" {
			if inner := structOf(f.Type()); inner != nil {
				ft := types.Unalias(f.Type())
				ptr, isPtr := ft.(*types.Pointer)
				if isPtr {
					ft = types.Unalias(ptr.Elem())
				}
				if n, ok := ft.(*types.Named); ok {
					b.note(n)
				}
				out = append(out, b.collectFields(inner, depth+1, viaPtr || isPtr, visiting)...)
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
		ft := types.Unalias(f.Type())
		ptr, isPtr := ft.(*types.Pointer)
		jf.optional = viaPtr || isPtr || slices.Contains(optList, "omitempty") || slices.Contains(optList, "omitzero")
		if slices.Contains(optList, "string") {
			if isPtr {
				ft = types.Unalias(ptr.Elem()) // encoding/json applies ,string through one pointer
			}
			if bt, ok := ft.Underlying().(*types.Basic); ok && bt.Info()&(types.IsNumeric|types.IsBoolean) != 0 {
				jf.asString = true
			}
		}
		out = append(out, jf)
	}
	return out
}
