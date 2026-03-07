package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

// APIRegistry holds registered APIs and their configuration
type APIRegistry struct {
	apis map[string]*APIDefinition
}

// APIDefinition defines a registered API
type APIDefinition struct {
	Name        string // e.g. "foo"
	Description string // Human-readable description of the API

	URLDefault   string // base url of the API, e.g. "http://127.0.0.1:8080/api"
	URLConfigKey string // key in config to customize URL, e.g. "foo.api.url"

	HasEvents bool // Whether this API has standardized /events endpoint
	HasHealth bool // Whether this API has a standardized /health endpoint
	HasStatus bool // Whether this API has a standardized /status endpoint

	EventsPath *string // Path to events endpoint, default: "/events"
	HealthPath *string // Path to health endpoint, default: "/health"
	StatusPath *string // Path to status endpoint, default: "/status"
}

func (a *APIDefinition) getHealthPath() string {
	if a.HealthPath != nil {
		return *a.HealthPath
	}
	return "/health"
}

func (a *APIDefinition) getStatusPath() string {
	if a.StatusPath != nil {
		return *a.StatusPath
	}
	return "/status"
}

func (a *APIDefinition) getEventsPath() string {
	if a.EventsPath != nil {
		return *a.EventsPath
	}
	return "/events"
}

// NewAPIRegistry creates a new API registry
func NewAPIRegistry() *APIRegistry {
	return &APIRegistry{
		apis: make(map[string]*APIDefinition),
	}
}

// Register adds an API to the registry
func (r *APIRegistry) Register(api APIDefinition) {
	r.apis[api.Name] = &api
}

// GetAll returns all registered APIs sorted by name
func (r *APIRegistry) GetAll() []*APIDefinition {
	apis := make([]*APIDefinition, 0, len(r.apis))
	for _, api := range r.apis {
		apis = append(apis, api)
	}

	// Sort by name
	sort.Slice(apis, func(i, j int) bool {
		return apis[i].Name < apis[j].Name
	})

	return apis
}

// APIStatus represents the health status of an API
type APIStatus struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Status       string `json:"status"`      // "healthy", "down", "unknown"
	ResponseTime int64  `json:"response_ms"` // Response time in milliseconds
	Error        string `json:"error,omitempty"`
}

// CheckHealth checks the health of an API
func (a *APIDefinition) CheckHealth(url string, timeout time.Duration) *APIStatus {
	status := &APIStatus{
		URL:    url,
		Status: "unknown",
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Build health endpoint URL
	healthURL := url + a.getHealthPath()

	// Measure response time
	start := time.Now()
	resp, err := client.Get(healthURL)
	elapsed := time.Since(start)

	if err != nil {
		status.Status = "down"
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()

	status.ResponseTime = elapsed.Milliseconds()

	// Check status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		status.Status = "healthy"
	} else {
		status.Status = "down"
		status.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return status
}

// apiProvider returns the built-in 'api' subcommand
func apiProvider(ctx *Context) []*cobra.Command {
	if ctx.APIs == nil || len(ctx.APIs.apis) == 0 {
		return nil
	}

	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "Manage and monitor registered APIs",
		Long:  "View and check the health status of registered Schemaf APIs.",
	}

	// Add 'list' subcommand
	var rawOutput bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered APIs and their status",
		Long:  "Check the health of all registered APIs and display their status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listAPIs(ctx, rawOutput)
		},
	}
	listCmd.Flags().BoolVar(&rawOutput, "raw", false, "Output raw JSON")

	apiCmd.AddCommand(listCmd)
	apiCmd.AddCommand(newEventsCommand(ctx))

	return []*cobra.Command{apiCmd}
}

// listAPIs lists all registered APIs with health checks
func listAPIs(ctx *Context, rawOutput bool) error {
	apis := ctx.APIs.GetAll()

	if len(apis) == 0 {
		Print("No APIs registered")
		return nil
	}

	// Check health of all APIs
	statuses := make([]*APIStatus, 0, len(apis))
	for _, api := range apis {
		url := ctx.Config.GetString(api.URLConfigKey)
		if url == "" {
			status := &APIStatus{
				Name:   api.Name,
				URL:    "<not configured>",
				Status: "unknown",
				Error:  fmt.Sprintf("config key '%s' not set", api.URLConfigKey),
			}
			statuses = append(statuses, status)
			continue
		}

		status := api.CheckHealth(url, 5*time.Second)
		status.Name = api.Name
		statuses = append(statuses, status)
	}

	// Output results
	if rawOutput {
		return outputJSON(statuses)
	}

	return outputTable(statuses)
}

// outputJSON outputs statuses as JSON
func outputJSON(statuses []*APIStatus) error {
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	Print(string(data))
	return nil
}

// outputTable outputs statuses as a formatted table
func outputTable(statuses []*APIStatus) error {
	// Calculate column widths
	maxNameLen := 4 // "NAME"
	maxURLLen := 3  // "URL"
	for _, s := range statuses {
		if len(s.Name) > maxNameLen {
			maxNameLen = len(s.Name)
		}
		if len(s.URL) > maxURLLen {
			maxURLLen = len(s.URL)
		}
	}

	// Print header
	header := fmt.Sprintf("%-*s  %-*s  %-8s  %s",
		maxNameLen, "NAME",
		maxURLLen, "URL",
		"STATUS", "RESPONSE")
	Print(header)
	Print(repeat("-", maxNameLen+maxURLLen+30))

	// Print rows
	for _, s := range statuses {
		statusStr := formatStatus(s.Status)
		responseStr := formatResponse(s)

		row := fmt.Sprintf("%-*s  %-*s  %-8s  %s",
			maxNameLen, s.Name,
			maxURLLen, s.URL,
			statusStr,
			responseStr)
		Print(row)

		// Show error if present
		if s.Error != "" {
			errorLine := fmt.Sprintf("%*s  %s%s%s",
				maxNameLen+maxURLLen+12, "",
				ColorRed, s.Error, ColorReset)
			Print(errorLine)
		}
	}

	return nil
}

// formatStatus formats the status with color
func formatStatus(status string) string {
	switch status {
	case "healthy":
		return ColorGreen + "✓ healthy" + ColorReset
	case "down":
		return ColorRed + "✗ down" + ColorReset
	case "unknown":
		return ColorYellow + "? unknown" + ColorReset
	default:
		return status
	}
}

// formatResponse formats the response time
func formatResponse(s *APIStatus) string {
	if s.Status != "healthy" {
		return "-"
	}
	return fmt.Sprintf("%dms", s.ResponseTime)
}

// repeat repeats a string n times
func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

// newEventsCommand creates the 'api events' subcommand
func newEventsCommand(ctx *Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Event stream operations",
		Long:  "Listen to and manage Schemaf event streams from registered APIs",
	}

	cmd.AddCommand(newListenCommand(ctx))
	return cmd
}

// newListenCommand creates the 'api events listen' subcommand
func newListenCommand(ctx *Context) *cobra.Command {
	var (
		apiName  string
		username string
		raw      bool
	)

	cmd := &cobra.Command{
		Use:   "listen [port]",
		Short: "Listen to Schemaf event stream",
		Long: `Connect to a Schemaf service WebSocket and stream events.

All Schemaf services expose events at /events endpoint using WebSocket.
Events follow the canonical format:
  {
    "event": "service.entity.action",
    "ts": "2026-02-28T12:00:00Z",
    "source": "schemaf-graph",
    "user": "florian",
    "payload": {...}
  }

Examples:
  schemaf api events listen 7110                 # Listen to port 7110
  schemaf api events listen --api graph          # Listen to registered 'graph' API
  schemaf api events listen 7110 -u florian      # Filter by user
  schemaf api events listen 7200 --raw           # Show raw JSON`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var port string
			if len(args) > 0 {
				port = args[0]
			} else if apiName != "" {
				// Look up API by name
				api := ctx.APIs.apis[apiName]
				if api == nil {
					return fmt.Errorf("API '%s' not registered", apiName)
				}
				// Extract port from URL
				apiURL := ctx.Config.GetString(api.URLConfigKey)
				if apiURL == "" {
					apiURL = api.URLDefault
				}
				u, err := url.Parse(apiURL)
				if err != nil {
					return fmt.Errorf("invalid API URL: %w", err)
				}
				port = u.Port()
				if port == "" {
					return fmt.Errorf("could not determine port from API URL: %s", apiURL)
				}
			} else {
				return fmt.Errorf("must specify either port or --api flag")
			}
			return listenToEvents(port, username, raw)
		},
	}

	cmd.Flags().StringVar(&apiName, "api", "", "API name to connect to (uses registered API URL)")
	cmd.Flags().StringVarP(&username, "user", "u", "", "Filter events by username")
	cmd.Flags().BoolVar(&raw, "raw", false, "Show raw JSON output")

	return cmd
}

// CanonicalEvent represents the standardized Schemaf event format
type CanonicalEvent struct {
	Event   string                 `json:"event"`
	TS      string                 `json:"ts"`
	Source  string                 `json:"source"`
	User    string                 `json:"user"`
	Payload map[string]interface{} `json:"payload"`
}

func listenToEvents(port, username string, raw bool) error {
	// Build WebSocket URL
	u := url.URL{Scheme: "ws", Host: "localhost:" + port, Path: "/events"}

	if !raw {
		Info("Connecting to %s", u.String())
		if username != "" {
			Info("Filtering events for user: %s", username)
		}
	}

	// Setup interrupt handler
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Connect to WebSocket
	headers := make(map[string][]string)
	if username != "" {
		headers["X-Schemaf-User"] = []string{username}
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to connect: %v (status: %d)", err, resp.StatusCode)
		}
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()

	if !raw {
		Info("Connected! Listening for events... (Ctrl+C to stop)")
		Print("")
	}

	// Channel for messages
	done := make(chan struct{})

	// Read messages
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					Error("WebSocket error: %v", err)
				}
				return
			}

			if raw {
				Print(string(message))
			} else {
				// Parse and pretty-print event
				var event CanonicalEvent
				if err := json.Unmarshal(message, &event); err != nil {
					Error("Failed to parse event: %v", err)
					Warning("Raw: %s", string(message))
					continue
				}

				printEvent(&event)
			}
		}
	}()

	// Wait for interrupt or connection close
	select {
	case <-done:
		if !raw {
			Info("Connection closed")
		}
	case <-interrupt:
		if !raw {
			Print("")
			Info("Shutting down...")
		}

		// Send close message
		err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			return err
		}

		// Wait for server to close connection
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}

	return nil
}

func printEvent(event *CanonicalEvent) {
	// Parse timestamp
	ts, err := time.Parse(time.RFC3339, event.TS)
	var timeStr string
	if err != nil {
		timeStr = event.TS
	} else {
		timeStr = ts.Format("15:04:05")
	}

	// Build formatted event string
	var output string
	output += ColorGray + "[" + timeStr + "]" + ColorReset + " "
	output += ColorBlue + event.Source + ColorReset + " "
	output += ColorGreen + event.Event + ColorReset + " "

	if event.User != "" {
		output += ColorYellow + "@" + event.User + ColorReset + " "
	}

	// Add payload
	if len(event.Payload) > 0 {
		payloadJSON, _ := json.Marshal(event.Payload)
		output += ColorGray + string(payloadJSON) + ColorReset
	}

	Print(output)
}
