package constants

import (
	"strings"
	"testing"
)

func TestParseInstance(t *testing.T) {
	for in, want := range map[string]int{
		"default": 0,
		"1":       1,
		"349":     349,
	} {
		got, err := ParseInstance(in)
		if err != nil || got != want {
			t.Errorf("ParseInstance(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "abc", "01", "+3", " 3", "3.0", "auto", "Default"} {
		if _, err := ParseInstance(in); err == nil {
			t.Errorf("ParseInstance(%q): want error", in)
		}
	}
}

// withConfig sets name, port and instance for one test and restores them.
func withConfig(t *testing.T, name string, p int) {
	t.Helper()
	oldName, oldPort, oldInst := projectName, port, instance
	t.Cleanup(func() { projectName, port, instance = oldName, oldPort, oldInst })
	projectName, port, instance = name, p, 0
}

func TestInstancePorts(t *testing.T) {
	withConfig(t, "myapp", 7100)

	// The default instance keeps the configured ports.
	if Port() != 7100 || FrontendPort() != 7102 || PostgresPort() != 7103 {
		t.Fatalf("default: %d/%d/%d", Port(), FrontendPort(), PostgresPort())
	}

	// The worked example from INSTALL.md › Parallel dev instances.
	if err := SetInstance(349); err != nil {
		t.Fatal(err)
	}
	if Port() != 10590 || FrontendPort() != 10592 || PostgresPort() != 10593 {
		t.Fatalf("instance 349: %d/%d/%d", Port(), FrontendPort(), PostgresPort())
	}
	if Instance() != 349 || ConfigPort() != 7100 {
		t.Fatalf("Instance()=%d ConfigPort()=%d", Instance(), ConfigPort())
	}
}

func TestInstancePortOverflow(t *testing.T) {
	withConfig(t, "myapp", 8000)

	// 8000 + 10×5752 = 65520 ≤ 65525: allowed.
	if err := SetInstance(5752); err != nil {
		t.Fatalf("5752: %v", err)
	}
	// 8000 + 10×5753 = 65530 > 65525: refused, and the instance is unchanged.
	err := SetInstance(5753)
	if err == nil || !strings.Contains(err.Error(), "65530") {
		t.Fatalf("5753: want overflow error naming 65530, got %v", err)
	}
	if Instance() != 5752 {
		t.Fatalf("instance changed on error: %d", Instance())
	}
}

func TestStackName(t *testing.T) {
	withConfig(t, "myapp", 8000)
	if StackName() != "myapp" || StackNameFor(0) != "myapp" {
		t.Fatalf("default: %q", StackName())
	}
	if err := SetInstance(349); err != nil {
		t.Fatal(err)
	}
	if StackName() != "myapp-349" || StackNameFor(7) != "myapp-7" {
		t.Fatalf("instance: %q %q", StackName(), StackNameFor(7))
	}
	if InstanceName(0) != "default" || InstanceName(349) != "349" {
		t.Fatalf("InstanceName: %q %q", InstanceName(0), InstanceName(349))
	}
}
