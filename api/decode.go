// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"fmt"
	"net/http"
	"reflect"
)

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
