// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a row of _schemaf_sessions, joined with the subject's
// revocation record.
type Session struct {
	ID        string
	Subject   string
	Provider  string // identity provider that started it; "" for api.StartSession
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt time.Time // zero if not revoked

	// SubjectRevokedBefore is _schemaf_revocations.revoked_before for the
	// session's subject; zero if the subject was never revoked.
	SubjectRevokedBefore time.Time
}

// SessionStore persists auth sessions in _schemaf_sessions and per-subject
// revocations in _schemaf_revocations. It satisfies api.SessionStore.
type SessionStore struct {
	db *sql.DB
}

// NewSessionStore returns a SessionStore on the given connection pool.
func NewSessionStore(d *sql.DB) *SessionStore {
	return &SessionStore{db: d}
}

// CreateSession inserts a new session.
func (s *SessionStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO _schemaf_sessions (id, subject, provider, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, sess.ID, sess.Subject, sess.Provider, sess.CreatedAt, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

// GetSession looks a session up by ID (one primary-key lookup, joined with
// the subject's revocation record). ok is false if no such session exists.
func (s *SessionStore) GetSession(ctx context.Context, id string) (Session, bool, error) {
	var sess Session
	var revokedAt, revokedBefore sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.subject, s.provider, s.created_at, s.expires_at, s.revoked_at, r.revoked_before
		FROM _schemaf_sessions s
		LEFT JOIN _schemaf_revocations r ON r.subject = s.subject
		WHERE s.id = $1
	`, id).Scan(&sess.ID, &sess.Subject, &sess.Provider, &sess.CreatedAt, &sess.ExpiresAt, &revokedAt, &revokedBefore)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("loading session: %w", err)
	}
	if revokedAt.Valid {
		sess.RevokedAt = revokedAt.Time
	}
	if revokedBefore.Valid {
		sess.SubjectRevokedBefore = revokedBefore.Time
	}
	return sess, true, nil
}

// RevokedBefore returns the subject's revoked_before, or the zero time if the
// subject was never revoked.
func (s *SessionStore) RevokedBefore(ctx context.Context, subject string) (time.Time, error) {
	var t time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT revoked_before FROM _schemaf_revocations WHERE subject = $1`, subject).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("loading revocation: %w", err)
	}
	return t, nil
}

// RevokeSession marks one session revoked. Revoking an unknown or already
// revoked session is not an error.
func (s *SessionStore) RevokeSession(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE _schemaf_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}

// RevokeSubject revokes every session of subject and records at as the
// subject's revoked_before, which also ends stateless tokens issued earlier.
func (s *SessionStore) RevokeSubject(ctx context.Context, subject string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("revoking subject: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO _schemaf_revocations (subject, revoked_before) VALUES ($1, $2)
		ON CONFLICT (subject) DO UPDATE
		SET revoked_before = GREATEST(_schemaf_revocations.revoked_before, EXCLUDED.revoked_before)
	`, subject, at); err != nil {
		return fmt.Errorf("revoking subject: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE _schemaf_sessions SET revoked_at = $2 WHERE subject = $1 AND revoked_at IS NULL`,
		subject, at); err != nil {
		return fmt.Errorf("revoking subject sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("revoking subject: %w", err)
	}
	return nil
}
