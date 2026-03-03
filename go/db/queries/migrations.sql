-- WARNING: This file is //go:embed embedded in the framework.
-- DO NOT reference via file path - it will not be accessible when atlas-base
-- is imported as a Go module dependency.
-- The Zeus CLI extracts embedded SQL during codegen to merge with project files.
-- name: GetAppliedMigrations :many
SELECT version FROM ab_migrations WHERE prefix = $1 ORDER BY version;

-- name: InsertMigration :exec
INSERT INTO ab_migrations (prefix, version, name) VALUES ($1, $2, $3);
