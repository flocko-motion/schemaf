-- WARNING: This file is //go:embed embedded in the framework.
-- DO NOT reference via file path - it will not be accessible when schemaf
-- is imported as a Go module dependency.
-- The Schemaf CLI extracts embedded SQL during codegen to merge with project files.
CREATE TABLE IF NOT EXISTS ab_migrations (
    id         SERIAL PRIMARY KEY,
    prefix     TEXT NOT NULL,
    version    INT NOT NULL,
    name       TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(prefix, version)
);
