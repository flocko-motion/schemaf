CREATE TABLE _schemaf_sessions (
    id         TEXT PRIMARY KEY,
    subject    TEXT NOT NULL,
    provider   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX _schemaf_sessions_subject_idx ON _schemaf_sessions (subject);

-- Per-subject revocation: every token (session or stateless) issued for the
-- subject before revoked_before is dead.
CREATE TABLE _schemaf_revocations (
    subject        TEXT PRIMARY KEY,
    revoked_before TIMESTAMPTZ NOT NULL
);
