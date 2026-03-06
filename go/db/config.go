package db

import (
	"context"
	"database/sql"
	"errors"
)

// ConfigGet retrieves a value from _schemaf_config. Returns ("", false, nil) if the key doesn't exist.
func ConfigGet(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := conn.QueryRowContext(ctx, `SELECT value FROM _schemaf_config WHERE key = $1`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// ConfigSet inserts or updates a key in _schemaf_config.
func ConfigSet(ctx context.Context, key, value string) error {
	_, err := conn.ExecContext(ctx, `
		INSERT INTO _schemaf_config (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, value)
	return err
}
