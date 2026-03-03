package cli

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"testing"

	"atlas.local/base/compose"
	basedb "atlas.local/base/db"
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
		HomeDir:    "/tmp/test-atlas",
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

// TestDB opens a database connection derived from an atlas compose file.
// It reads the x-atlas metadata (project name, db-port, dev-db-pass) from
// the resolved compose files so tests never need to hardcode credentials or
// set DATABASE_URL manually.
//
// Falls back to DATABASE_URL if set (CI / external Postgres).
// Registers t.Cleanup to close the connection automatically.
//
// Example:
//
//	db := cli.TestDB(t, "../compose/test.yml")
func TestDB(t *testing.T, composeFile string) *sql.DB {
	t.Helper()

	// If DATABASE_URL is explicitly set, use it (CI override).
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		db, err := basedb.Open(dsn)
		if err != nil {
			t.Fatalf("TestDB: open from DATABASE_URL: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}

	// Resolve the compose file to extract x-atlas metadata.
	files, err := compose.Resolve([]string{composeFile})
	if err != nil {
		t.Fatalf("TestDB: resolve %q: %v", composeFile, err)
	}

	// Find the entry file's x-atlas extension (last in resolution order).
	var atlas *compose.AtlasExtension
	for i := len(files) - 1; i >= 0; i-- {
		if files[i].Atlas != nil && files[i].Atlas.Project != "" {
			atlas = files[i].Atlas
			break
		}
	}
	if atlas == nil {
		t.Fatalf("TestDB: no x-atlas.project found in %q", composeFile)
	}

	port := atlas.DBPort
	if port == 0 {
		t.Fatalf("TestDB: x-atlas.db-port not set in %q", composeFile)
	}

	pass := atlas.DevDBPass
	if pass == "" {
		pass = "dev"
	}

	dsn := fmt.Sprintf(
		"postgres://atlas:%s@127.0.0.1:%d/%s?sslmode=disable",
		pass, port, atlas.Project,
	)

	db, err := basedb.Open(dsn)
	if err != nil {
		t.Fatalf("TestDB: open %s: %v", dsn, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
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
