# Zeus CLI Framework

Composable CLI framework for projects built on atlas-base. Each project ships **one** CLI binary (called Zeus) that mounts the framework subcommands plus project-specific commands.

## Overview

The framework provides:

- Configuration + state management (project home)
- HTTP client utilities
- Output formatting helpers
- Subcommand provider pattern for composability
- API registry for health monitoring
- Built-in subcommands (compose, codegen)

## Project Discovery (Documentation-First)

Zeus discovers the project by walking upward from the directory containing `zeus.sh` until it finds `project.toml`. The file defines:

- `name`: short, lowercase project name (used for directory paths)
- `title`: human-friendly project title

From this, Zeus derives the project home directory:

- `~/.<project>/etc/` and `~/.<project>/var/`
- In dev mode: `~/.<project>/dev/etc/` and `~/.<project>/dev/var/`

This behavior is documentation-first and will be implemented as the framework matures.

## Usage

### Creating a CLI Binary (Project Zeus)

```go
package main

import (
    "os"
    "path/filepath"
    
    basecli "atlas.local/base/cli"
    "atlas.local/base/cli/cmd/compose"
    "atlas.local/base/cli/cmd/codegen"
    "your.project/cli/cmd"
)

func main() {
    home, _ := os.UserHomeDir()
    projectHome := filepath.Join(home, ".your-project")

    // Create framework instance
    cli, err := basecli.New(projectHome)
    if err != nil {
        panic(err)
    }

    // Mount base subcommands + project subcommands
    cli.AddSubcommands(
        compose.SubcommandProvider,
        codegen.SubcommandProvider,
        cmd.ProjectSubcommandProvider,
    )

    // Execute
    cli.Execute()
}
```

### Creating a Subcommand Provider

```go
package cli

import (
    cli "atlas.local/base/cli"
    "github.com/spf13/cobra"
)

// Provider returns subcommands to mount
func Provider(ctx *cli.Context) []*cobra.Command {
    return []*cobra.Command{
        newLearnCommand(ctx),
        newSearchCommand(ctx),
    }
}

func newLearnCommand(ctx *cli.Context) *cobra.Command {
    return &cobra.Command{
        Use:   "learn <fact>",
        Short: "Teach the system a fact",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Access config (namespaced by component)
            apiURL := ctx.Config.GetString("backend.api_url")
            
            // Access state (namespaced by component)
            user := ctx.State.GetString("current_user")
            
            // Use HTTP client
            resp, err := ctx.HTTPClient.Post(apiURL + "/learn", ...)
            
            // Use formatting utilities
            cli.Success("Fact learned!")
            
            return nil
        },
    }
}
```

## Configuration Convention

**IMPORTANT**: All config/state keys MUST be namespaced by component name.

### Config File (`~/.<project>/etc/config.toml`)

```toml
# Global settings (no namespace needed)
project_root = "/path/to/project"

# Component-specific settings (namespaced)
[backend]
api_url = "http://localhost:7110"
users = ["user1", "user2"]

[gateway]
api_url = "http://localhost:7120"

[foo]
default_limit = 20
```

Access in code:
```go
// Use component.key pattern
apiURL := ctx.Config.GetString("backend.api_url")
users := ctx.Config.GetStringSlice("backend.users")
```

### State File (`~/.<project>/etc/state.toml`)

State is **extensible** - any component can add its own keys.

```toml
# Global state
current_user = "flopriv"

# Component-specific state (namespaced)
[backend]
last_search = "2026-03-01T13:00:00Z"
last_import = "/path/to/file.json"

[foo]
last_sync = "2026-03-01T12:00:00Z"

[custom_component]
custom_state = "value"
```

Access in code:
```go
// Read state
user := ctx.State.GetString("current_user")
lastSearch := ctx.State.GetString("backend.last_search")

// Write state
ctx.State.Set("backend.last_import", "/new/path.json")
ctx.State.Save()
```

## API Registry

The framework includes an API registry for monitoring service health.

### Registering APIs

```go
// Register an API with default /health endpoint
cli.RegisterAPI("backend", "backend.api_url")

// Register an API with custom health endpoint
cli.RegisterAPIWithHealth("custom", "custom.api_url", "/api/status")
```

This automatically adds an `api` subcommand to your CLI:

```bash
# List all APIs with health status
zeus api list

# Output:
# NAME   URL                    STATUS      RESPONSE
# --------------------------------------------------------
# backend http://localhost:7110  ✓ healthy   3ms
# gateway http://localhost:7120  ✗ down      -
#                                           Get "http://localhost:7120/health": connection refused

# Raw JSON output
zeus api list --raw

# Output:
# [
#   {
#     "name": "backend",
#     "url": "http://localhost:7110",
#     "status": "healthy",
#     "response_ms": 3
#   },
#   {
#     "name": "gateway",
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

APIs are configured in `~/.<project>/etc/config.toml`:

```toml
[backend]
api_url = "http://localhost:7110"

[gateway]
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
    HomeDir    string       // project home directory
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
cli.Success(format string, args ...interface{})
cli.Error(format string, args ...interface{})
cli.Warning(format string, args ...interface{})
cli.Info(format string, args ...interface{})
cli.JSON(v interface{}, pretty bool) error
cli.Table(headers []string, rows [][]string)
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
    ctx := &cli.Context{
        Config: &cli.Config{...},
        State: &cli.State{...},
        HTTPClient: cli.NewHTTPClient(false),
    }
    
    // Get commands from provider
    commands := Provider(ctx)
    
    // Test command execution
    ...
}
```

## Best Practices

1. **Always namespace config/state keys** by component name (e.g., `backend.api_url`)
2. **Use ctx.State.Save()** after modifying state
3. **Use formatting utilities** for consistent output
4. **Return errors** from RunE, don't os.Exit()
5. **Keep providers simple** - just return commands
6. **Put logic in command files** - keep provider.go minimal

## Related Docs

- `README.md`
- `compose/README.md`
- `docs/CODEGEN.md`
