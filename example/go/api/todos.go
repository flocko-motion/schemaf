package api

import (
	"context"
	"time"

	schemafapi "schemaf.local/base/api"
)

// ─── Shared types ──────────────────────────────────────────────────────────────

// Todo represents a single todo item.
type Todo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── List todos ────────────────────────────────────────────────────────────────

// ListTodosEndpoint returns all todo items ordered by creation date.
// Returns an empty array if no todos exist.
type ListTodosEndpoint struct{}

func (e ListTodosEndpoint) Method() string { return "GET" }
func (e ListTodosEndpoint) Path() string   { return "/api/todos" }
func (e ListTodosEndpoint) Auth() bool     { return false }
func (e ListTodosEndpoint) Handle(ctx context.Context, req ListTodosReq) (ListTodosResp, error) {
	// TODO: query db using generated db.ListTodos(ctx)
	return ListTodosResp{Todos: []Todo{}}, nil
}

type ListTodosReq struct{}

type ListTodosResp struct {
	Todos []Todo `json:"todos"`
}

// ─── Get todo ──────────────────────────────────────────────────────────────────

// GetTodoEndpoint retrieves a single todo item by ID.
// Returns 404 if the todo does not exist.
type GetTodoEndpoint struct{}

func (e GetTodoEndpoint) Method() string { return "GET" }
func (e GetTodoEndpoint) Path() string   { return "/api/todos/{id}" }
func (e GetTodoEndpoint) Auth() bool     { return false }
func (e GetTodoEndpoint) Handle(ctx context.Context, req GetTodoReq) (GetTodoResp, error) {
	// TODO: query db using generated db.GetTodo(ctx, req.ID)
	if req.ID == "" {
		return GetTodoResp{}, schemafapi.ErrNotFound
	}
	return GetTodoResp{Todo: Todo{ID: req.ID}}, nil
}

type GetTodoReq struct {
	ID string `path:"id"`
}

type GetTodoResp struct {
	Todo Todo `json:"todo"`
}

// ─── Create todo ───────────────────────────────────────────────────────────────

// CreateTodoEndpoint creates a new todo item.
type CreateTodoEndpoint struct{}

func (e CreateTodoEndpoint) Method() string { return "POST" }
func (e CreateTodoEndpoint) Path() string   { return "/api/todos" }
func (e CreateTodoEndpoint) Auth() bool     { return false }
func (e CreateTodoEndpoint) Handle(ctx context.Context, req CreateTodoReq) (Todo, error) {
	// TODO: query db using generated db.CreateTodo(ctx, req.Text)
	return Todo{ID: "stub", Text: req.Text, Done: false, CreatedAt: time.Now()}, nil
}

type CreateTodoReq struct {
	Text string `json:"text"`
}

// ─── Update todo ───────────────────────────────────────────────────────────────

// UpdateTodoEndpoint updates the text and done status of a todo item.
type UpdateTodoEndpoint struct{}

func (e UpdateTodoEndpoint) Method() string { return "PUT" }
func (e UpdateTodoEndpoint) Path() string   { return "/api/todos/{id}" }
func (e UpdateTodoEndpoint) Auth() bool     { return false }
func (e UpdateTodoEndpoint) Handle(ctx context.Context, req UpdateTodoReq) (Todo, error) {
	// TODO: query db using generated db.UpdateTodo(ctx, ...)
	return Todo{ID: req.ID, Text: req.Text, Done: req.Done, CreatedAt: time.Now()}, nil
}

type UpdateTodoReq struct {
	ID   string `path:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// ─── Delete todo ───────────────────────────────────────────────────────────────

// DeleteTodoEndpoint deletes a todo item by ID.
type DeleteTodoEndpoint struct{}

func (e DeleteTodoEndpoint) Method() string { return "DELETE" }
func (e DeleteTodoEndpoint) Path() string   { return "/api/todos/{id}" }
func (e DeleteTodoEndpoint) Auth() bool     { return false }
func (e DeleteTodoEndpoint) Handle(ctx context.Context, req DeleteTodoReq) (DeleteTodoResp, error) {
	// TODO: query db using generated db.DeleteTodo(ctx, req.ID)
	return DeleteTodoResp{}, nil
}

type DeleteTodoReq struct {
	ID string `path:"id"`
}

type DeleteTodoResp struct{}
