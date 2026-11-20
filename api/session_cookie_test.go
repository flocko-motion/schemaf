package api

import (
	"testing"

	"github.com/flocko-motion/schemaf/constants"
)

func TestCookieNamePerInstance(t *testing.T) {
	oldName := ""
	if constants.IsProjectNameSet() {
		oldName = constants.ProjectName()
	}
	oldInst := constants.Instance()
	t.Cleanup(func() {
		constants.SetProjectName(oldName)
		_ = constants.SetInstance(oldInst)
	})
	constants.SetProjectName("myapp")

	if err := constants.SetInstance(0); err != nil {
		t.Fatal(err)
	}
	if got := cookieName(); got != "myapp_session" {
		t.Errorf("default instance: %q, want myapp_session", got)
	}

	if err := constants.SetInstance(349); err != nil {
		t.Fatal(err)
	}
	if got := cookieName(); got != "myapp-349_session" {
		t.Errorf("instance 349: %q, want myapp-349_session", got)
	}
}
