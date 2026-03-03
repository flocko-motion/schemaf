CREATE TABLE IF NOT EXISTS ab_migrations (
    id         SERIAL PRIMARY KEY,
    prefix     TEXT NOT NULL,
    version    INT NOT NULL,
    name       TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(prefix, version)
);
