// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package constants

import (
	"fmt"
	"strconv"
)

// DefaultInstance is the name of the instance without a number: the dev stack
// on the configured port with today's names (INSTALL.md › Parallel dev instances).
const DefaultInstance = "default"

// ParseInstance parses an instance name: "default" (returned as 0) or a whole
// number ≥ 1 in canonical decimal form. Anything else is an error.
func ParseInstance(s string) (int, error) {
	if s == DefaultInstance {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || strconv.Itoa(n) != s {
		return 0, fmt.Errorf("invalid instance %q: use a whole number ≥ 1 (e.g. the issue number) or %q", s, DefaultInstance)
	}
	return n, nil
}

// InstancePortStep is the distance between the base ports of neighbouring
// instances: instance n's base port is port + InstancePortStep × n.
const InstancePortStep = 10

// MaxInstanceBasePort is the highest allowed instance base port, leaving room
// for the offsets above it (65535 − 10).
const MaxInstanceBasePort = 65535 - InstancePortStep

// instance is the current dev instance; 0 is the default instance. Only dev
// commands set it (schemaf.App resolves it from --instance / .schemaf/instance).
var instance int

// Instance returns the current instance number; 0 is the default instance.
func Instance() int { return instance }

// SetInstance selects the dev instance (0 = default). It fails, leaving the
// instance unchanged, when the instance's base port would exceed MaxInstanceBasePort.
func SetInstance(n int) error {
	if _, err := InstanceBasePort(port, n); err != nil {
		return err
	}
	instance = n
	return nil
}

// InstanceBasePort returns port + 10 × n, or an error when it exceeds MaxInstanceBasePort.
func InstanceBasePort(port, n int) (int, error) {
	base := port + InstancePortStep*n
	if base > MaxInstanceBasePort {
		return 0, fmt.Errorf("instance %d would use base port %d (port %d + %d × %d), above the maximum %d — pick a smaller instance number",
			n, base, port, InstancePortStep, n, MaxInstanceBasePort)
	}
	return base, nil
}

// InstanceName returns the display name of instance n: "default" or the number.
func InstanceName(n int) string {
	if n == 0 {
		return DefaultInstance
	}
	return strconv.Itoa(n)
}

// StackName returns the current instance's name prefix for compose project,
// containers, network and volume: "<name>" for the default instance,
// "<name>-<n>" for instance n.
func StackName() string { return StackNameFor(instance) }

// StackNameFor is StackName for instance n.
func StackNameFor(n int) string {
	if n == 0 {
		return ProjectName()
	}
	return ProjectName() + "-" + strconv.Itoa(n)
}
