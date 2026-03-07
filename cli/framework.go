package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CLI represents the composable CLI framework
type CLI struct {
	root    *cobra.Command
	ctx     *Context
	opts    *cliOptions
	homeDir string
}

// New creates a new CLI framework instance
func New(homeDir string, opts ...Option) (*CLI, error) {
	// Apply options
	cliOpts := &cliOptions{
		verbose: false,
		version: "dev",
	}
	for _, opt := range opts {
		opt(cliOpts)
	}

	// Load config and state
	config, err := loadConfig(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	state, err := loadState(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	// Create HTTP client
	httpClient := NewHTTPClient(cliOpts.verbose)

	// Create API registry
	apiRegistry := NewAPIRegistry()

	// Create context
	ctx := &Context{
		APIs:       apiRegistry,
		Config:     config,
		State:      state,
		HTTPClient: httpClient,
		HomeDir:    homeDir,
		Verbose:    cliOpts.verbose,
	}

	// Create root command
	rootCmd := &cobra.Command{
		Use:     "schemaf",
		Short:   "Schemaf CLI - unified tooling for the Schemaf ecosystem",
		Long:    "Schemaf CLI provides commands for interacting with Schemaf services.",
		Version: cliOpts.version,
	}

	// Add persistent flags
	rootCmd.PersistentFlags().BoolVar(&ctx.Verbose, "verbose", false, "Enable verbose output")

	cli := &CLI{
		root:    rootCmd,
		ctx:     ctx,
		opts:    cliOpts,
		homeDir: homeDir,
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

	// Return new CLI instance with this subcommand as root
	return &CLI{
		root:    subCmd,
		ctx:     c.ctx,
		opts:    c.opts,
		homeDir: c.homeDir,
	}
}

// Add mounts subcommand providers to this CLI
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

	// Auto-mount the api commands if this is the first API registered
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
