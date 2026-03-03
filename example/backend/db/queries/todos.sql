-- name: ListTodos :many
SELECT * FROM todos ORDER BY created_at DESC;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = $1;

-- name: CreateTodo :one
INSERT INTO todos (text) VALUES ($1) RETURNING *;

-- name: UpdateTodo :one
UPDATE todos SET text = $2, done = $3 WHERE id = $1 RETURNING *;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = $1;
