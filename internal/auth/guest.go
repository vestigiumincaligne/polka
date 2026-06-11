package auth

import (
	"context"
	"encoding/hex"
	"time"
)

// Guest accounts for demo mode: every browser session gets an
// ephemeral user so ratings, lists and progress work, and a cleanup
// job wipes the account (with all its data) when it gets stale.

const guestPrefix = "guest-"

// CreateGuest creates an ephemeral demo user with a ready session.
// Returns the session token to be set as a cookie. Guests never log in
// with a password (only the session cookie), so we store a disabled
// hash placeholder instead of running argon2 — that keeps guest
// creation cheap and unusable for password login.
func (s *Service) CreateGuest(ctx context.Context) (string, *User, error) {
	login := guestPrefix + hex.EncodeToString(randomBytes(8))
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (login, password_hash, display_name, role) VALUES (?, '!', 'Guest', ?)`,
		login, RoleUser)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	u, err := s.GetByID(ctx, id)
	if err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(randomBytes(32))
	expires := time.Now().UTC().Add(24 * time.Hour)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash(token), u.ID, expires.Format(time.RFC3339)); err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// CleanupGuests deletes guest accounts older than maxAge together with
// their sessions, progress, ratings and lists (FK cascade). Returns the
// number of accounts removed.
func (s *Service) CleanupGuests(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM users WHERE login LIKE ? AND datetime(created_at) <= datetime(?)`,
		guestPrefix+"%", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
