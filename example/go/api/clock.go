package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GetServerTimeEndpoint returns the current time from the clock sidecar.
// Demonstrates how project compose extensions integrate with the backend.
type GetServerTimeEndpoint struct{}

func (e GetServerTimeEndpoint) Method() string { return "GET" }
func (e GetServerTimeEndpoint) Path() string   { return "/api/time" }
func (e GetServerTimeEndpoint) Auth() bool     { return false }
func (e GetServerTimeEndpoint) Handle(ctx context.Context, _ GetServerTimeReq) (GetServerTimeResp, error) {
	resp, err := http.Get("http://clock:8080")
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
