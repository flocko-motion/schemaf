-- name: GetAppliedMigrations :many
SELECT version FROM ab_migrations WHERE prefix = $1 ORDER BY version;

-- name: InsertMigration :exec
INSERT INTO ab_migrations (prefix, version, name) VALUES ($1, $2, $3);
