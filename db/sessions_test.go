// Real-Postgres tests for the session store. Skipped unless DATABASE_URL is
// set; run via e2e/db-test.sh.
package db

import (
	"context"
	"testing"
	"time"
)

func TestSessionStore(t *testing.T) {
	d := openIntegrationDB(t)
	ctx := context.Background()
	resetSchema(t, ctx, d)
	if err := RunSet(ctx, d, MigrationSet{Prefix: "schemaf", Files: frameworkMigrations}); err != nil {
		t.Fatalf("framework migrations: %v", err)
	}
	if !regclassExists(t, ctx, d, "_schemaf_sessions") || !regclassExists(t, ctx, d, "_schemaf_revocations") {
		t.Fatal("session tables missing after framework migrations")
	}

	s := NewSessionStore(d)
	t0 := time.Now().Truncate(time.Microsecond)
	mk := func(id, subject string) Session {
		return Session{ID: id, Subject: subject, Provider: "auth0", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}
	}
	for _, sess := range []Session{mk("a1", "alice"), mk("a2", "alice"), mk("b1", "bob")} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}

	got, ok, err := s.GetSession(ctx, "a1")
	if err != nil || !ok {
		t.Fatalf("GetSession: %v, %v", ok, err)
	}
	if got.Subject != "alice" || got.Provider != "auth0" || !got.ExpiresAt.Equal(t0.Add(time.Hour)) ||
		!got.RevokedAt.IsZero() || !got.SubjectRevokedBefore.IsZero() {
		t.Errorf("GetSession = %+v", got)
	}
	if _, ok, err := s.GetSession(ctx, "missing"); ok || err != nil {
		t.Errorf("missing session: ok=%v err=%v", ok, err)
	}

	t.Run("RevokeSession", func(t *testing.T) {
		if err := s.RevokeSession(ctx, "a1", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		a1, _, _ := s.GetSession(ctx, "a1")
		a2, _, _ := s.GetSession(ctx, "a2")
		if a1.RevokedAt.IsZero() || !a2.RevokedAt.IsZero() {
			t.Errorf("a1 revoked=%v a2 revoked=%v", a1.RevokedAt, a2.RevokedAt)
		}
		// Revoking again keeps the first revocation time.
		s.RevokeSession(ctx, "a1", t0.Add(time.Hour))
		again, _, _ := s.GetSession(ctx, "a1")
		if !again.RevokedAt.Equal(a1.RevokedAt) {
			t.Errorf("revoked_at moved: %v → %v", a1.RevokedAt, again.RevokedAt)
		}
	})

	t.Run("RevokeSubject", func(t *testing.T) {
		at := t0.Add(2 * time.Minute)
		if err := s.RevokeSubject(ctx, "alice", at); err != nil {
			t.Fatal(err)
		}
		a2, _, _ := s.GetSession(ctx, "a2")
		if a2.RevokedAt.IsZero() || !a2.SubjectRevokedBefore.Equal(at) {
			t.Errorf("a2 after RevokeSubject = %+v", a2)
		}
		b1, _, _ := s.GetSession(ctx, "b1")
		if !b1.RevokedAt.IsZero() || !b1.SubjectRevokedBefore.IsZero() {
			t.Errorf("bob affected: %+v", b1)
		}
		// Stateless tokens are checked against revoked_before.
		rb, err := s.RevokedBefore(ctx, "alice")
		if err != nil || !rb.Equal(at) {
			t.Errorf("RevokedBefore(alice) = %v, %v", rb, err)
		}
		if rb, _ := s.RevokedBefore(ctx, "bob"); !rb.IsZero() {
			t.Errorf("RevokedBefore(bob) = %v", rb)
		}
		// revoked_before never moves backwards.
		s.RevokeSubject(ctx, "alice", t0)
		if rb, _ := s.RevokedBefore(ctx, "alice"); !rb.Equal(at) {
			t.Errorf("revoked_before moved backwards to %v", rb)
		}
		// A session created after the revocation is unaffected.
		later := mk("a3", "alice")
		later.CreatedAt = at.Add(time.Second)
		s.CreateSession(ctx, later)
		a3, _, _ := s.GetSession(ctx, "a3")
		if !a3.RevokedAt.IsZero() || !a3.CreatedAt.After(a3.SubjectRevokedBefore) {
			t.Errorf("new session after revocation = %+v", a3)
		}
	})
}
