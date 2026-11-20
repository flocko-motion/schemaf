package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func paramReq(rawQuery string, path map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/x?"+rawQuery, nil)
	for k, v := range path {
		r.SetPathValue(k, v)
	}
	return r
}

func mustDecode(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := decodeParams(r, v); err != nil {
		t.Fatalf("decodeParams: %v", err)
	}
}

func wantDecodeErr(t *testing.T, r *http.Request, v any, contains string) {
	t.Helper()
	err := decodeParams(r, v)
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("err = %v, want one containing %q", err, contains)
	}
}

func TestDecodeQueryParams(t *testing.T) {
	type role string
	var got struct {
		ContentClass string `query:"content_class"`
		Limit        int    `query:"limit"`
		Small        int8   `query:"small"`
		Big          uint64 `query:"big"`
		Verbose      bool   `query:"verbose"`
		Role         role   `query:"role"`
	}
	mustDecode(t, paramReq("content_class=source&limit=10&small=-3&big=7&verbose=true&role=admin", nil), &got)
	if got.ContentClass != "source" || got.Limit != 10 || got.Small != -3 || got.Big != 7 || !got.Verbose || got.Role != "admin" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeQueryRequiredMissing(t *testing.T) {
	var got struct {
		Name  string `query:"name"`
		Count int    `query:"count"`
	}
	wantDecodeErr(t, paramReq("count=1", nil), &got, `query param "name" is required`)
}

func TestDecodeQueryEmptyValue(t *testing.T) {
	var s struct {
		Search string `query:"search"`
	}
	s.Search = "preset"
	mustDecode(t, paramReq("search=", nil), &s)
	if s.Search != "" {
		t.Errorf("Search = %q, want empty", s.Search)
	}
	var n struct {
		N int `query:"n"`
	}
	wantDecodeErr(t, paramReq("n=", nil), &n, `query param "n"`)
	var b struct {
		B bool `query:"b"`
	}
	wantDecodeErr(t, paramReq("b=", nil), &b, `query param "b"`)
}

func TestDecodeQueryRepeatedScalar(t *testing.T) {
	var got struct {
		Sort string `query:"sort"`
	}
	wantDecodeErr(t, paramReq("sort=a&sort=b", nil), &got, `query param "sort" is sent 2 times`)
	var ptr struct {
		Sort *string `query:"sort"`
	}
	wantDecodeErr(t, paramReq("sort=a&sort=b", nil), &ptr, `query param "sort" is sent 2 times`)
}

func TestDecodeQueryPointer(t *testing.T) {
	type req struct {
		Name  *string `query:"name"`
		Limit *int    `query:"limit"`
	}
	var got req
	mustDecode(t, paramReq("name=hello&limit=5", nil), &got)
	if got.Name == nil || *got.Name != "hello" || got.Limit == nil || *got.Limit != 5 {
		t.Errorf("got %+v", got)
	}
	got = req{}
	mustDecode(t, paramReq("", nil), &got)
	if got.Name != nil || got.Limit != nil {
		t.Errorf("got %+v, want nil pointers", got)
	}
	wantDecodeErr(t, paramReq("limit=x", nil), &got, `query param "limit": invalid integer`)
}

func TestDecodeQuerySlice(t *testing.T) {
	type req struct {
		Tags []string `query:"tag"`
		IDs  []int    `query:"id"`
		Bits []bool   `query:"bit"`
	}
	var got req
	mustDecode(t, paramReq("tag=a&tag=b&id=1&id=2&id=3&bit=true", nil), &got)
	if !reflect.DeepEqual(got, req{Tags: []string{"a", "b"}, IDs: []int{1, 2, 3}, Bits: []bool{true}}) {
		t.Errorf("got %+v", got)
	}
	got = req{}
	mustDecode(t, paramReq("", nil), &got)
	if got.Tags == nil || len(got.Tags) != 0 || got.IDs == nil || len(got.IDs) != 0 {
		t.Errorf("got %#v, want empty non-nil slices", got)
	}
	wantDecodeErr(t, paramReq("id=1&id=x", nil), &got, `query param "id": invalid integer`)
}

func TestDecodeQueryUndeclaredIgnored(t *testing.T) {
	var got struct {
		A *string `query:"a"`
	}
	mustDecode(t, paramReq("other=1&other=2", nil), &got)
}

func TestDecodePathTyped(t *testing.T) {
	var got struct {
		ID   int64  `path:"id"`
		Slug string `path:"slug"`
		On   bool   `path:"on"`
	}
	mustDecode(t, paramReq("", map[string]string{"id": "42", "slug": "abc", "on": "true"}), &got)
	if got.ID != 42 || got.Slug != "abc" || !got.On {
		t.Errorf("got %+v", got)
	}
	wantDecodeErr(t, paramReq("", map[string]string{"id": "abc", "slug": "x", "on": "true"}), &got, `path param "id": invalid integer`)
	wantDecodeErr(t, paramReq("", map[string]string{"slug": "x", "on": "true"}), &got, `path param "id": no value`)
}

// --- through the endpoint pipeline ---

type paramsReq struct {
	ID    int      `path:"id"`
	Sort  string   `query:"sort"`
	Limit *int     `query:"limit"`
	Name  string   `json:"name"`
	Tags  []string `query:"tag"`
}

type paramsEndpoint struct{}

func (paramsEndpoint) Method() string  { return "PUT" }
func (paramsEndpoint) Path() string    { return "/api/test/params/{id}" }
func (paramsEndpoint) Auth() AuthLevel { return AuthNone }
func (paramsEndpoint) Handle(_ context.Context, req paramsReq) (paramsReq, error) {
	return req, nil
}

func TestParamsThroughEndpoint(t *testing.T) {
	srv := serve(t, NewRoute[paramsReq, paramsReq](paramsEndpoint{}, "", ""))
	ct := map[string]string{"Content-Type": "application/json"}

	// The body names every field: path and query values still win.
	resp, body := do(t, "PUT", srv.URL+"/api/test/params/7?sort=name&tag=a",
		`{"ID": 99, "Sort": "evil", "Limit": 5, "Tags": ["x"], "name": "n"}`, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}
	var got paramsReq
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Sort != "name" || got.Limit != nil || got.Name != "n" || !reflect.DeepEqual(got.Tags, []string{"a"}) {
		t.Errorf("got %+v", got)
	}

	for _, c := range []struct{ url, msg string }{
		{"/api/test/params/7", `query param "sort" is required`},
		{"/api/test/params/abc?sort=x", `path param "id": invalid integer "abc"`},
		{"/api/test/params/7?sort=a&sort=b", `query param "sort" is sent 2 times; send it once`},
	} {
		resp, body := do(t, "PUT", srv.URL+c.url, "", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", c.url, resp.StatusCode)
		}
		var eb errBody
		if err := json.Unmarshal([]byte(body), &eb); err != nil || eb.Code != "bad_request" || eb.Error != c.msg {
			t.Errorf("%s: body %s, want code bad_request, error %q", c.url, body, c.msg)
		}
	}
}
