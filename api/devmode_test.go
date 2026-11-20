package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDevModeReportedInStatus(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		set      bool
		wantDev  bool
		wantMode string
	}{
		{name: "unset is production", set: false, wantDev: false, wantMode: "production"},
		{name: "empty is production", value: "", set: true, wantDev: false, wantMode: "production"},
		{name: "1 is development", value: "1", set: true, wantDev: true, wantMode: "development"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SCHEMAF_DEV", "placeholder")
			if tc.set {
				t.Setenv("SCHEMAF_DEV", tc.value)
			} else {
				os.Unsetenv("SCHEMAF_DEV") // t.Setenv above restores it after the test
			}

			if got := DevMode(); got != tc.wantDev {
				t.Errorf("DevMode() = %v, want %v", got, tc.wantDev)
			}

			rec := httptest.NewRecorder()
			StatusHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/status", nil))
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding /api/status: %v", err)
			}
			if body["mode"] != tc.wantMode {
				t.Errorf(`/api/status "mode" = %v, want %q`, body["mode"], tc.wantMode)
			}
		})
	}
}
