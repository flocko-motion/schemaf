package schemaf

import (
	"context"
	"strings"
	"testing"
)

func TestRunRefusesInvalidDevModeValue(t *testing.T) {
	for _, v := range []string{"true", "yes", "0", " 1"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SCHEMAF_DEV", v)
			a := &App{ctx: context.Background(), project: "myapp"}
			err := a.Run()
			if err == nil {
				t.Fatalf("Run() with SCHEMAF_DEV=%q: want error, got nil", v)
			}
			for _, want := range []string{"SCHEMAF_DEV", `"1"`, v} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestAuthTokenRefusesOutsideDevMode: minting tokens is dev-only, including
// for a native process outside compose.
func TestAuthTokenRefusesOutsideDevMode(t *testing.T) {
	t.Setenv("SCHEMAF_DEV", "")
	t.Setenv("SCHEMAF_ENV", "") // native, outside compose: must not decide the mode
	a := &App{ctx: context.Background(), project: "myapp"}
	cmd := a.authTokenCmd()
	cmd.SetArgs([]string{"alice"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "dev-only") {
		t.Fatalf("auth token in production: got %v, want dev-only refusal", err)
	}
}
