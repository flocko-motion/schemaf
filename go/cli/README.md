# Atlas CLI Framework

Composable CLI framework for building unified Atlas tooling.

## Overview

This framework provides:
- Configuration management (config.toml)
- State management (state.toml)
- HTTP client utilities
- Output formatting helpers
- Subcommand provider pattern for composability
- API registry for health monitoring

## Usage

### Creating a CLI Binary

```go
package main

import (
    "os"
    "path/filepath"
    
    atlascli "github.com/mlpa/atlas-base/go/cli"
    graphcli "github.com/mlpa/atlas-graph/src/cli"
)

func main() {
    home, _ := os.UserHomeDir()
    atlasHome := filepath.Join(home, ".atlas")
    
    // Create framework instance
    cli, err := atlascli.New(atlasHome,
        atlascli.WithVersion("1.0.0"),
    )
    if err != nil {
        panic(err)
    }
    
    // Register APIs for health monitoring
    cli.RegisterAPI("graph", "graph.api_url")
    cli.RegisterAPI("vault", "vault.api_url")
    
    // Mount subcommands
    cli.Subcommand("graph", "Graph operations", "...").
        Add(graphcli.Provider)
    
    // Execute
    cli.Execute()
}
```

### Creating a Subcommand Provider

```go
package cli

import (
    atlascli "github.com/mlpa/atlas-base/go/cli"
    "github.com/spf13/cobra"
)

// Provider returns subcommands to mount
func Provider(ctx *atlascli.Context) []*cobra.Command {
    return []*cobra.Command{
        newLearnCommand(ctx),
        newSearchCommand(ctx),
    }
}

func newLearnCommand(ctx *atlascli.Context) *cobra.Command {
    return &cobra.Command{
        Use:   "learn <fact>",
        Short: "Teach the graph a fact",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Access config (namespaced by component)
            apiURL := ctx.Config.GetString("graph.api_url")
            
            // Access state (namespaced by component)
            user := ctx.State.GetString("current_user")
            
            // Use HTTP client
            resp, err := ctx.HTTPClient.Post(apiURL + "/learn", ...)
            
            // Use formatting utilities
            atlascli.PrintSuccess("Fact learned!")
            
            return nil
        },
    }
}
```

## Configuration Convention

**IMPORTANT**: All config/state keys MUST be namespaced by component name.

### Config File (`~/.atlas/config.toml`)

```toml
# Global settings (no namespace needed)
atlas_repo = "/r/priv/atlas"

# Component-specific settings (namespaced)
[graph]
api_url = "http://localhost:7110"
users = ["user1", "user2"]

[vault]
api_url = "http://localhost:7120"
sync_enabled = true

[psyche]
api_url = "http://localhost:7200"
model = "gpt-4"
```

Access in code:
```go
// Use component.key pattern
apiURL := ctx.Config.GetString("graph.api_url")
users := ctx.Config.GetStringSlice("graph.users")
```

### State File (`~/.atlas/state.toml`)

State is **extensible** - any component can add its own keys.

```toml
# Global state
current_user = "flopriv"

# Component-specific state (namespaced)
[graph]
last_search = "2026-03-01T13:00:00Z"
last_import = "/path/to/file.json"

[vault]
last_sync = "2026-03-01T12:00:00Z"

[custom_component]
custom_state = "value"
```

Access in code:
```go
// Read state
user := ctx.State.GetString("current_user")
lastSearch := ctx.State.GetString("graph.last_search")

// Write state
ctx.State.Set("graph.last_import", "/new/path.json")
ctx.State.Save()
```

## API Registry

The framework includes an API registry for monitoring service health.

### Registering APIs

```go
// Register an API with default /health endpoint
cli.RegisterAPI("graph", "graph.api_url")

// Register an API with custom health endpoint
cli.RegisterAPIWithHealth("custom", "custom.api_url", "/api/status")
```

This automatically adds an `api` subcommand to your CLI:

```bash
# List all APIs with health status
atlas api list

# Output:
# NAME   URL                    STATUS      RESPONSE
# --------------------------------------------------------
# graph  http://localhost:7110  ✓ healthy   3ms
# vault  http://localhost:7120  ✗ down      -
#                                           Get "http://localhost:7120/health": connection refused

# Raw JSON output
atlas api list --raw

# Output:
# [
#   {
#     "name": "graph",
#     "url": "http://localhost:7110",
#     "status": "healthy",
#     "response_ms": 3
#   },
#   {
#     "name": "vault",
#     "url": "http://localhost:7120",
#     "status": "down",
#     "response_ms": 0,
#     "error": "Get \"http://localhost:7120/health\": connection refused"
#   }
# ]
```

### Status Values

- **✓ healthy** (green) - API is responding with 2xx status
- **✗ down** (red) - API is not responding or returning error status
- **? unknown** (yellow) - API URL not configured in config.toml

### Configuration

APIs are configured in `~/.atlas/config.toml`:

```toml
[graph]
api_url = "http://localhost:7110"

[vault]
api_url = "http://localhost:7120"
```

## API Reference

### Context

Providers receive a `Context` with access to framework features:

```go
type Context struct {
    APIs       *APIRegistry // API registry for health monitoring
    Config     *Config      // Config from config.toml
    State      *State       // State from state.toml
    HTTPClient *HTTPClient  // HTTP client with utilities
    HomeDir    string       // ~/.atlas path
    Verbose    bool         // --verbose flag
}
```

### Config Methods

```go
config.Get(key string) interface{}
config.GetString(key string) string
config.GetInt(key string) int
config.GetBool(key string) bool
config.GetStringSlice(key string) []string
```

### State Methods

```go
state.Get(key string) interface{}
state.GetString(key string) string
state.Set(key string, value interface{})
state.Save() error
```

### HTTP Client Methods

```go
client.Get(url string) (*Response, error)
client.Post(url string, body interface{}) (*Response, error)
client.WithTimeout(duration time.Duration) *HTTPClient
client.WithHeader(key, value string) *HTTPClient
```

### Formatting Utilities

```go
atlascli.PrintSuccess(message string)
atlascli.PrintError(err error)
atlascli.PrintWarning(message string)
atlascli.PrintInfo(message string)
atlascli.PrintJSON(v interface{}, pretty bool) error
atlascli.PrintTable(headers []string, rows [][]string)
```

## Migration from JSON to TOML

The framework automatically migrates `state.json` to `state.toml` on first run:
- Reads old JSON state
- Converts to TOML format
- Writes `state.toml`
- Removes `state.json`
- Prints confirmation message

## Testing

```go
func TestProvider(t *testing.T) {
    // Create test context
    ctx := &atlascli.Context{
        Config: &atlascli.Config{...},
        State: &atlascli.State{...},
        HTTPClient: atlascli.NewHTTPClient(false),
    }
    
    // Get commands from provider
    commands := Provider(ctx)
    
    // Test command execution
    ...
}
```

## Best Practices

1. **Always namespace config/state keys** by component name (e.g., `graph.api_url`)
2. **Use ctx.State.Save()** after modifying state
3. **Use formatting utilities** for consistent output
4. **Return errors** from RunE, don't os.Exit()
5. **Keep providers simple** - just return commands
6. **Put logic in command files** - keep provider.go minimal
