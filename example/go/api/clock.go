// clock.go — Example endpoint that calls a sidecar Docker service.
// Demonstrates compose extensions (compose/clock.yml) integrated with the backend.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// GetServerTimeEndpoint returns the current time from the clock sidecar.
// Demonstrates how project compose extensions integrate with the backend.
type GetServerTimeEndpoint struct{}

func (e GetServerTimeEndpoint) Method() string { return "GET" }
func (e GetServerTimeEndpoint) Path() string   { return "/api/time" }
func (e GetServerTimeEndpoint) Auth() bool     { return false }
func (e GetServerTimeEndpoint) Handle(ctx context.Context, _ GetServerTimeReq) (GetServerTimeResp, error) {
	clockURL := os.Getenv("CLOCK_URL")
	if clockURL == "" {
		clockURL = "http://clock:8080"
	}

	resp, err := http.Get(clockURL)
	if err != nil {
		return GetServerTimeResp{}, fmt.Errorf("calling clock service: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Time time.Time `json:"time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return GetServerTimeResp{}, fmt.Errorf("decoding clock response: %w", err)
	}

	return GetServerTimeResp{Time: payload.Time}, nil
}

type GetServerTimeReq struct{}

type GetServerTimeResp struct {
	Time time.Time `json:"time"`
}
