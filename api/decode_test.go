package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDecodeQueryParams(t *testing.T) {
	type req struct {
		ContentClass string `query:"content_class"`
		Limit        int    `query:"limit"`
		Verbose      bool   `query:"verbose"`
	}

	r := &http.Request{URL: &url.URL{RawQuery: "content_class=source&limit=10&verbose=true"}}

	var got req
	if err := decodeQueryParams(r, &got); err != nil {
		t.Fatalf("decodeQueryParams: %v", err)
	}

	if got.ContentClass != "source" {
		t.Errorf("ContentClass = %q, want %q", got.ContentClass, "source")
	}
	if got.Limit != 10 {
		t.Errorf("Limit = %d, want %d", got.Limit, 10)
	}
	if got.Verbose != true {
		t.Errorf("Verbose = %v, want %v", got.Verbose, true)
	}
}

func TestDecodeQueryParams_Missing(t *testing.T) {
	type req struct {
		Name  string `query:"name"`
		Count int    `query:"count"`
	}

	r := &http.Request{URL: &url.URL{RawQuery: ""}}

	var got req
	if err := decodeQueryParams(r, &got); err != nil {
		t.Fatalf("decodeQueryParams: %v", err)
	}

	if got.Name != "" {
		t.Errorf("Name = %q, want empty", got.Name)
	}
	if got.Count != 0 {
		t.Errorf("Count = %d, want 0", got.Count)
	}
}

func TestDecodeQueryParams_Pointer(t *testing.T) {
	type req struct {
		Name  *string `query:"name"`
		Limit *int    `query:"limit"`
	}

	r := &http.Request{URL: &url.URL{RawQuery: "name=hello&limit=5"}}

	var got req
	if err := decodeQueryParams(r, &got); err != nil {
		t.Fatalf("decodeQueryParams: %v", err)
	}

	if got.Name == nil || *got.Name != "hello" {
		t.Errorf("Name = %v, want pointer to %q", got.Name, "hello")
	}
	if got.Limit == nil || *got.Limit != 5 {
		t.Errorf("Limit = %v, want pointer to %d", got.Limit, 5)
	}
}

func TestDecodeQueryParams_PointerMissing(t *testing.T) {
	type req struct {
		Name *string `query:"name"`
	}

	r := &http.Request{URL: &url.URL{RawQuery: ""}}

	var got req
	if err := decodeQueryParams(r, &got); err != nil {
		t.Fatalf("decodeQueryParams: %v", err)
	}

	if got.Name != nil {
		t.Errorf("Name = %v, want nil", got.Name)
	}
}

func TestDecodeQueryParams_InvalidInt(t *testing.T) {
	type req struct {
		Limit int `query:"limit"`
	}

	r := &http.Request{URL: &url.URL{RawQuery: "limit=abc"}}

	var got req
	if err := decodeQueryParams(r, &got); err == nil {
		t.Fatal("expected error for invalid int, got nil")
	}
}
