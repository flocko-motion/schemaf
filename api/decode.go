// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
)

// decodeQueryParams populates fields tagged with `query:"name"` from r.URL.Query().
// Supports string, int, and bool fields.
func decodeQueryParams(r *http.Request, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return nil
	}
	query := r.URL.Query()
	rt := rv.Type()
	for i := range rt.NumField() {
		field := rt.Field(i)
		tag := field.Tag.Get("query")
		if tag == "" {
			continue
		}
		val := query.Get(tag)
		if val == "" {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(val)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return fmt.Errorf("query param %q: invalid integer: %w", tag, err)
			}
			fv.SetInt(n)
		case reflect.Bool:
			b, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("query param %q: invalid boolean: %w", tag, err)
			}
			fv.SetBool(b)
		default:
			return fmt.Errorf("query param %q: unsupported type %s", tag, fv.Kind())
		}
	}
	return nil
}

// decodePathParams populates fields tagged with `path:"name"` from r.PathValue.
// Only string fields are supported for now.
func decodePathParams(r *http.Request, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	for i := range rt.NumField() {
		field := rt.Field(i)
		tag := field.Tag.Get("path")
		if tag == "" {
			continue
		}
		val := r.PathValue(tag)
		if val == "" {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if fv.Kind() != reflect.String {
			return fmt.Errorf("path param %q: only string fields supported, got %s", tag, fv.Kind())
		}
		fv.SetString(val)
	}
	return nil
}
