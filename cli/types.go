// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// SubcommandProvider returns a slice of commands to be mounted
// Providers receive a Context with access to config, state, and utilities
type SubcommandProvider func(ctx *Context) []*cobra.Command

// ApiProvider registers APIs with the API registry
type ApiProvider func() []APIDefinition

// Context provides access to framework features
type Context struct {
	APIs       *APIRegistry
	Config     *Config
	State      *State
	HTTPClient *HTTPClient
	HomeDir    string
	Verbose    bool
}

// Config handles config.toml (static configuration)
type Config struct {
	v *viper.Viper
}

// Get returns a value from config
func (c *Config) Get(key string) interface{} {
	return c.v.Get(key)
}

// GetString returns a string value from config
func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

// GetInt returns an int value from config
func (c *Config) GetInt(key string) int {
	return c.v.GetInt(key)
}

// GetBool returns a bool value from config
func (c *Config) GetBool(key string) bool {
	return c.v.GetBool(key)
}

// GetStringSlice returns a string slice from config
func (c *Config) GetStringSlice(key string) []string {
	return c.v.GetStringSlice(key)
}

// State handles state.toml (dynamic runtime state - extensible)
type State struct {
	v    *viper.Viper
	path string
}

// Get returns a value from state
func (s *State) Get(key string) interface{} {
	return s.v.Get(key)
}

// GetString returns a string value from state
func (s *State) GetString(key string) string {
	return s.v.GetString(key)
}

// Set sets a value in state
func (s *State) Set(key string, value interface{}) {
	s.v.Set(key, value)
}

// Save persists state to disk
func (s *State) Save() error {
	return s.v.WriteConfig()
}

// HTTPClient provides HTTP request utilities
type HTTPClient struct {
	client  *http.Client
	headers map[string]string
	verbose bool
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// Option for functional options pattern
type Option func(*cliOptions)

// Internal options struct
type cliOptions struct {
	verbose bool
	version string
}
