// Package auth — users, passwords and sessions. Stored in a separate
// users.db database so that re-importing the collection does not affect accounts.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vestigiumincaligne/polka/internal/sqlitedrv"
	"golang.org/x/crypto/argon2"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	sessionTTL = 7 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid login or password")
	ErrNotFound           = errors.New("user not found")
	ErrLoginTaken         = errors.New("login already exists")
	ErrLastAdmin          = errors.New("cannot remove the last administrator")
)

type User struct {
	ID          int64
	Login       string
	DisplayName string
	Role        string
	Disabled    bool
	CreatedAt   string
}

func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

type Service struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	login         TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	display_name  TEXT NOT NULL DEFAULT '',
	role          TEXT NOT NULL DEFAULT 'user',
	disabled      INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	expires_at TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
`

func Open(path string) (*Service, error) {
	db, err := sqlitedrv.Open(path, sqlitedrv.Options{
		WAL: true, BusyTimeout: 10000, ForeignKeys: true,
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema + progressSchema + settingsSchema + listsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth schema: %w", err)
	}
	migrateProgress(db)
	return &Service{db: db}, nil
}

func (s *Service) Close() error { return s.db.Close() }

// --- Passwords: argon2id, parameters embedded in the hash ---

func hashPassword(password string) string {
	salt := randomBytes(16)
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(key))
}

func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := hex.DecodeString(parts[1])
	want, err2 := hex.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand must not fail
	}
	return b
}

// RandomPassword returns a random password for initial setup.
func RandomPassword() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := randomBytes(14)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// --- Sessions ---

// EnsureLogin returns the user by login, creating it if
// missing (for desktop mode with auto-login).
func (s *Service) EnsureLogin(ctx context.Context, login, displayName, role string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, login, display_name, role, disabled, created_at
		FROM users WHERE login = ?`, login).
		Scan(&u.ID, &u.Login, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt)
	if err == nil {
		return &u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return s.CreateUser(ctx, login, RandomPassword(), displayName, role)
}

// Verify checks login/password without opening a session (HTTP Basic for OPDS).
func (s *Service) Verify(ctx context.Context, login, password string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, login, password_hash, display_name, role, disabled, created_at
		FROM users WHERE login = ?`, strings.TrimSpace(login)).
		Scan(&u.ID, &u.Login, &hash, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (u.Disabled || !verifyPassword(password, hash))) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Login verifies credentials and opens a session.
func (s *Service) Login(ctx context.Context, login, password string) (string, *User, error) {
	u, err := s.Verify(ctx, login, password)
	if err != nil {
		return "", nil, err
	}

	token := hex.EncodeToString(randomBytes(32))
	expires := time.Now().UTC().Add(sessionTTL)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash(token), u.ID, expires.Format(time.RFC3339)); err != nil {
		return "", nil, err
	}
	return token, u, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash(token))
	return err
}

// GetByToken returns the session's user. A session past half of its
// lifetime gets extended (sliding renewal).
func (s *Service) GetByToken(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var u User
	var expiresStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.login, u.display_name, u.role, u.disabled, u.created_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, tokenHash(token)).
		Scan(&u.ID, &u.Login, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt, &expiresStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	expires, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil || time.Now().UTC().After(expires) || u.Disabled {
		s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash(token))
		return nil, ErrNotFound
	}

	if time.Until(expires) < sessionTTL/2 {
		newExpires := time.Now().UTC().Add(sessionTTL)
		s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token_hash = ?`,
			newExpires.Format(time.RFC3339), tokenHash(token))
	}
	return &u, nil
}

// --- User management ---

func (s *Service) UsersCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Service) adminsBeside(ctx context.Context, excludeID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE role = ? AND disabled = 0 AND id != ?`,
		RoleAdmin, excludeID).Scan(&n)
	return n, err
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, login, display_name, role, disabled, created_at
		FROM users ORDER BY login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Login, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Service) CreateUser(ctx context.Context, login, password, displayName, role string) (*User, error) {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return nil, errors.New("login and password are required")
	}
	if role != RoleAdmin {
		role = RoleUser
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (login, password_hash, display_name, role) VALUES (?, ?, ?, ?)`,
		login, hashPassword(password), strings.TrimSpace(displayName), role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrLoginTaken
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetByID(ctx, id)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, login, display_name, role, disabled, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Login, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// UpdateUser changes the provided fields (nil — leave as is). Demoting
// or disabling the last administrator is forbidden.
func (s *Service) UpdateUser(ctx context.Context, id int64, password, displayName, role *string, disabled *bool) error {
	u, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}

	demoting := role != nil && *role != RoleAdmin && u.Role == RoleAdmin
	disabling := disabled != nil && *disabled && !u.Disabled
	if u.Role == RoleAdmin && (demoting || disabling) {
		if n, err := s.adminsBeside(ctx, id); err != nil {
			return err
		} else if n == 0 {
			return ErrLastAdmin
		}
	}

	if password != nil && *password != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET password_hash = ? WHERE id = ?`, hashPassword(*password), id); err != nil {
			return err
		}
		// A password change resets the user's other sessions.
		s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
	}
	if displayName != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET display_name = ? WHERE id = ?`, strings.TrimSpace(*displayName), id); err != nil {
			return err
		}
	}
	if role != nil {
		r := RoleUser
		if *role == RoleAdmin {
			r = RoleAdmin
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, r, id); err != nil {
			return err
		}
	}
	if disabled != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET disabled = ? WHERE id = ?`, boolToInt(*disabled), id); err != nil {
			return err
		}
		if *disabled {
			s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
		}
	}
	return nil
}

func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	u, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin {
		if n, err := s.adminsBeside(ctx, id); err != nil {
			return err
		} else if n == 0 {
			return ErrLastAdmin
		}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
