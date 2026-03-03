package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	baseapi "atlas.local/base/api"
	basedb "atlas.local/base/db"
)

// Todo represents a todo item.
type Todo struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateTodoRequest is the request body for creating a todo.
type CreateTodoRequest struct {
	Text string `json:"text"`
}

// UpdateTodoRequest is the request body for updating a todo.
type UpdateTodoRequest struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

func init() {
	baseapi.Register(baseapi.Route{Method: "GET", Path: "/api/todos", Summary: "List todos", Handler: http.HandlerFunc(listTodos)})
	baseapi.Register(baseapi.Route{Method: "POST", Path: "/api/todos", Summary: "Create todo", Handler: http.HandlerFunc(createTodo), ReqType: &CreateTodoRequest{}})
	baseapi.Register(baseapi.Route{Method: "GET", Path: "/api/todos/{id}", Summary: "Get todo", Handler: http.HandlerFunc(getTodo)})
	baseapi.Register(baseapi.Route{Method: "PUT", Path: "/api/todos/{id}", Summary: "Update todo", Handler: http.HandlerFunc(updateTodo), ReqType: &UpdateTodoRequest{}})
	baseapi.Register(baseapi.Route{Method: "DELETE", Path: "/api/todos/{id}", Summary: "Delete todo", Handler: http.HandlerFunc(deleteTodo)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func pathID(r *http.Request) string {
	// Go 1.22+ net/http path value extraction
	id := r.PathValue("id")
	if id != "" {
		return id
	}
	// Fallback: last segment of URL path
	parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
	return parts[len(parts)-1]
}

func listTodos(w http.ResponseWriter, r *http.Request) {
	rows, err := basedb.QueryContext(r.Context(),
		`SELECT id, text, done, created_at FROM todos ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var todos []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.ID, &t.Text, &t.Done, &t.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		todos = append(todos, t)
	}
	if todos == nil {
		todos = []Todo{}
	}
	writeJSON(w, http.StatusOK, todos)
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	var req CreateTodoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var t Todo
	err := basedb.QueryRowContext(r.Context(),
		`INSERT INTO todos (text) VALUES ($1) RETURNING id, text, done, created_at`,
		req.Text,
	).Scan(&t.ID, &t.Text, &t.Done, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func getTodo(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var t Todo
	err := basedb.QueryRowContext(r.Context(),
		`SELECT id, text, done, created_at FROM todos WHERE id = $1`, id,
	).Scan(&t.ID, &t.Text, &t.Done, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req UpdateTodoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var t Todo
	err := basedb.QueryRowContext(r.Context(),
		`UPDATE todos SET text = $2, done = $3 WHERE id = $1 RETURNING id, text, done, created_at`,
		id, req.Text, req.Done,
	).Scan(&t.ID, &t.Text, &t.Done, &t.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	res, err := basedb.ExecContext(r.Context(), `DELETE FROM todos WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
