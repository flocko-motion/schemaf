-- migrations/ — SQL migrations, auto-discovered by codegen and embedded into the binary.
-- Name files with numeric prefix for ordering (0001_, 0002_, ...).
-- Migrations run automatically on server startup. See EXTEND.md#database.
CREATE TABLE todos (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    text       TEXT NOT NULL,
    done       BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
