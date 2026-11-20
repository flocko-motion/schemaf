// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"bytes"
	"io"
	"os"

	"github.com/spf13/viper"
)

// NewTestContext creates a Context for testing with in-memory config/state
func NewTestContext() *Context {
	// Create in-memory config
	configViper := viper.New()
	configViper.SetConfigType("toml")

	// Create in-memory state
	stateViper := viper.New()
	stateViper.SetConfigType("toml")

	return &Context{
		Config:     &Config{v: configViper},
		State:      &State{v: stateViper, path: ":memory:"},
		HTTPClient: NewHTTPClient(false),
		HomeDir:    "/tmp/test-schemaf",
		Verbose:    false,
	}
}

// SetTestConfig sets config values for testing
func (ctx *Context) SetTestConfig(key string, value interface{}) {
	ctx.Config.v.Set(key, value)
}

// SetTestState sets state values for testing
func (ctx *Context) SetTestState(key string, value interface{}) {
	ctx.State.v.Set(key, value)
}

// CaptureOutput captures stdout and stderr for testing
func CaptureOutput(f func()) (stdout, stderr string) {
	// Capture stdout
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	// Capture stderr
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	// Create channels to read from pipes
	outC := make(chan string)
	errC := make(chan string)

	// Start reading goroutines
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	// Run the function
	f()

	// Restore stdout/stderr
	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Get captured output
	stdout = <-outC
	stderr = <-errC

	return
}
