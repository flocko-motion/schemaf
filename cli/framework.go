// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/files"
)

// CLI represents the composable CLI framework
type CLI struct {
	root *cobra.Command
	ctx  *Context
	opts *cliOptions
}

// New creates a new CLI framework instance
func New(opts ...Option) (*CLI, error) {
	cliOpts := &cliOptions{
		verbose: false,
		version: "dev",
	}
	for _, opt := range opts {
		opt(cliOpts)
	}

	// ProjectHome panics if project name isn't set yet (first run, before codegen).
	// Only codegen can run without it — all other commands need the project name.
	var homeDir string
	var config *Config
	var state *State
	isBootstrap := len(os.Args) > 1 && (os.Args[1] == "codegen" || os.Args[1] == "prerun" || os.Args[1] == "init")
	func() {
		if isBootstrap {
			defer func() { recover() }()
		}
		homeDir = files.ProjectHome()
		config, _ = loadConfig(homeDir)
		state, _ = loadState(homeDir)
	}()

	// Create HTTP client
	httpClient := NewHTTPClient(cliOpts.verbose)

	// Create API registry
	apiRegistry := NewAPIRegistry()

	ctx := &Context{
		APIs:       apiRegistry,
		Config:     config,
		State:      state,
		HTTPClient: httpClient,
		HomeDir:    homeDir,
		Verbose:    cliOpts.verbose,
	}

	rootCmd := &cobra.Command{
		Use:     "schemaf",
		Short:   "Schemaf CLI - unified tooling for the Schemaf ecosystem",
		Long:    "Schemaf CLI provides commands for interacting with Schemaf services.",
		Version: cliOpts.version,
	}

	rootCmd.PersistentFlags().BoolVar(&ctx.Verbose, "verbose", false, "Enable verbose output")

	cli := &CLI{
		root: rootCmd,
		ctx:  ctx,
		opts: cliOpts,
	}

	return cli, nil
}

// Subcommand creates and returns a new subcommand group
func (c *CLI) Subcommand(use, short, long string) *CLI {
	subCmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
	}

	c.root.AddCommand(subCmd)

	return &CLI{
		root: subCmd,
		ctx:  c.ctx,
		opts: c.opts,
	}
}

// AddSubcommands mounts subcommand providers to this CLI
func (c *CLI) AddSubcommands(providers ...SubcommandProvider) *CLI {
	for _, provider := range providers {
		commands := provider(c.ctx)
		for _, cmd := range commands {
			c.root.AddCommand(cmd)
		}
	}
	return c
}

func (c *CLI) AddApis(apiProviders ...ApiProvider) *CLI {
	hadAPIs := len(c.ctx.APIs.apis) > 0

	for _, provider := range apiProviders {
		apis := provider()
		for _, api := range apis {
			c.ctx.APIs.Register(api)
		}
	}

	if !hadAPIs && len(c.ctx.APIs.apis) > 0 {
		c.AddSubcommands(apiProvider)
	}

	return c
}

// Execute runs the CLI
func (c *CLI) Execute() error {
	return c.root.Execute()
}

// Root returns the root command (useful for testing)
func (c *CLI) Root() *cobra.Command {
	return c.root
}

// Context returns the CLI context (useful for providers)
func (c *CLI) Context() *Context {
	return c.ctx
}

// WithVerbose sets verbose mode
func WithVerbose(verbose bool) Option {
	return func(opts *cliOptions) {
		opts.verbose = verbose
	}
}

// WithVersion sets the version string
func WithVersion(version string) Option {
	return func(opts *cliOptions) {
		opts.version = version
	}
}
