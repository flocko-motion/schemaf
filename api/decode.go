// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// decodeParams fills the fields of the struct v points to that are tagged
// `path:"name"` (from r.PathValue) or `query:"name"` (from the query
// string), its own and those of the structs it embeds by value (without a
// json name), at any depth. See EXTEND.md, "Path and query parameters":
//   - path and non-pointer scalar query fields are required;
//   - a pointer query field not sent is nil, a slice not sent is empty;
//   - a scalar or pointer query param sent more than once is an error;
//   - values are parsed by the field's type (string, integers, bool);
//   - an embedded pointer to a struct with parameters is an error.
//
// It runs after the body is decoded, so it overwrites whatever the body set.
func decodeParams(r *http.Request, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return nil
	}
	return decodeStructParams(r, r.URL.Query(), rv)
}

// decodeStructParams fills the parameter fields of struct rv and of the
// structs it embeds by value without a json name (see decodeParams).
func decodeStructParams(r *http.Request, query url.Values, rv reflect.Value) error {
	rt := rv.Type()
	for i := range rt.NumField() {
		field := rt.Field(i)
		fv := rv.Field(i)
		pathName, queryName := field.Tag.Get("path"), field.Tag.Get("query")
		if field.Anonymous && pathName == "" && queryName == "" && !jsonNamed(field) {
			ft := field.Type
			if ft.Kind() == reflect.Ptr && ft.Elem().Kind() == reflect.Struct {
				if hasParamFields(ft.Elem()) {
					return fmt.Errorf("embedded pointer %s in %s has path or query fields: embed it by value", ft, rt)
				}
				continue
			}
			if ft.Kind() == reflect.Struct {
				// Exported fields of an unexported embed are still settable.
				if err := decodeStructParams(r, query, fv); err != nil {
					return err
				}
				continue
			}
		}
		if !fv.CanSet() {
			continue
		}
		if pathName != "" {
			if err := setPathParam(fv, pathName, r.PathValue(pathName)); err != nil {
				return err
			}
		}
		if queryName != "" {
			vals, sent := query[queryName]
			if err := setQueryParam(fv, queryName, vals, sent); err != nil {
				return err
			}
		}
	}
	return nil
}

// jsonNamed reports whether f's json tag gives it a name (or "-"): an
// embedded struct with one is a nested object, not part of the outer type.
func jsonNamed(f reflect.StructField) bool {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name != ""
}

// hasParamFields reports whether struct type t, or a struct it embeds, has
// a field tagged `path` or `query`.
func hasParamFields(t reflect.Type) bool {
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("path") != "" || f.Tag.Get("query") != "" {
			return true
		}
		if f.Anonymous && !jsonNamed(f) && f.Type.Kind() == reflect.Struct && hasParamFields(f.Type) {
			return true
		}
	}
	return false
}

func setPathParam(fv reflect.Value, name, val string) error {
	what := fmt.Sprintf("path param %q", name)
	if val == "" {
		// The mux never routes an empty {name} segment: the request didn't
		// come through it, or Path() has no {name}. Don't leave the field
		// to whatever the body set.
		return fmt.Errorf("%s: no value — the route has no {%s} segment", what, name)
	}
	if fv.Kind() == reflect.Ptr || fv.Kind() == reflect.Slice {
		return fmt.Errorf("%s: a path field must be a string, integer or bool, got %s", what, fv.Type())
	}
	return setScalar(fv, what, val)
}

func setQueryParam(fv reflect.Value, name string, vals []string, sent bool) error {
	what := fmt.Sprintf("query param %q", name)
	switch fv.Kind() {
	case reflect.Slice:
		out := reflect.MakeSlice(fv.Type(), len(vals), len(vals))
		for i, val := range vals {
			if err := setScalar(out.Index(i), what, val); err != nil {
				return err
			}
		}
		fv.Set(out)
		return nil
	case reflect.Ptr:
		if !sent {
			fv.Set(reflect.Zero(fv.Type()))
			return nil
		}
		ptr := reflect.New(fv.Type().Elem())
		if err := setSingle(ptr.Elem(), what, vals); err != nil {
			return err
		}
		fv.Set(ptr)
		return nil
	}
	if !sent {
		return fmt.Errorf("%s is required", what)
	}
	return setSingle(fv, what, vals)
}

// setSingle sets fv from the one value of a param that takes one value.
func setSingle(fv reflect.Value, what string, vals []string) error {
	if len(vals) > 1 {
		return fmt.Errorf("%s is sent %d times; send it once", what, len(vals))
	}
	return setScalar(fv, what, vals[0])
}

// setScalar parses val into a string, integer or bool fv.
func setScalar(fv reflect.Value, what, val string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("%s: invalid integer %q", what, val)
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(val, 10, fv.Type().Bits())
		if err != nil {
			return fmt.Errorf("%s: invalid integer %q", what, val)
		}
		fv.SetUint(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%s: invalid boolean %q", what, val)
		}
		fv.SetBool(b)
	default:
		return fmt.Errorf("%s: unsupported type %s", what, fv.Type())
	}
	return nil
}
