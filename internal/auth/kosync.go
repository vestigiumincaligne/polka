// KOReader progress sync (kosync protocol): per-user, per-document
// reading positions pushed by KOReader devices. Documents are identified
// by KOReader's partial-MD5 digest of the book file; the mapping between
// digests and catalog books lives in the collection database.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const kosyncSchema = `
CREATE TABLE IF NOT EXISTS kosync_progress (
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	document   TEXT NOT NULL,
	progress   TEXT NOT NULL DEFAULT '',
	percentage REAL NOT NULL DEFAULT 0,
	device     TEXT NOT NULL DEFAULT '',
	device_id  TEXT NOT NULL DEFAULT '',
	timestamp  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, document)
) WITHOUT ROWID;
`

// KosyncProgress is one document position as KOReader reported it.
type KosyncProgress struct {
	Document   string
	Progress   string  // an xpointer for reflowable books, a page number for paged ones
	Percentage float64 // 0..1
	Device     string
	DeviceID   string
	Timestamp  int64 // unix seconds, server-side
}

func kosyncKeySetting(userID int64) string { return fmt.Sprintf("kosync_key:%d", userID) }

// KosyncKey returns the user's device key (an MD5 hex of the password
// entered on the device); empty means sync is not set up.
func (s *Service) KosyncKey(ctx context.Context, userID int64) string {
	return s.GetSetting(ctx, kosyncKeySetting(userID), "")
}

// SetKosyncKey stores the device key; an empty key disables sync.
func (s *Service) SetKosyncKey(ctx context.Context, userID int64, key string) error {
	return s.SetSetting(ctx, kosyncKeySetting(userID), key)
}

// GetByLogin finds an active (not disabled) user by login.
func (s *Service) GetByLogin(ctx context.Context, login string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, login, display_name, role, disabled, created_at
		FROM users WHERE login = ?`, strings.TrimSpace(login)).
		Scan(&u.ID, &u.Login, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, ErrNotFound
	}
	return u, nil
}

// KosyncUpdate stores a document position and returns the server timestamp.
func (s *Service) KosyncUpdate(ctx context.Context, userID int64, p *KosyncProgress) (int64, error) {
	ts := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kosync_progress (user_id, document, progress, percentage, device, device_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, document) DO UPDATE SET
			progress = excluded.progress, percentage = excluded.percentage,
			device = excluded.device, device_id = excluded.device_id,
			timestamp = excluded.timestamp`,
		userID, p.Document, p.Progress, p.Percentage, p.Device, p.DeviceID, ts)
	return ts, err
}

// KosyncGet returns the stored position or ErrNotFound.
func (s *Service) KosyncGet(ctx context.Context, userID int64, document string) (*KosyncProgress, error) {
	p := &KosyncProgress{Document: document}
	err := s.db.QueryRowContext(ctx, `
		SELECT progress, percentage, device, device_id, timestamp
		FROM kosync_progress WHERE user_id = ? AND document = ?`, userID, document).
		Scan(&p.Progress, &p.Percentage, &p.Device, &p.DeviceID, &p.Timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// SaveOverall updates only the overall fraction of the reading progress
// (a device that knows nothing about our chapters reported the position);
// chapter, position and locator of an existing record are preserved.
func (s *Service) SaveOverall(ctx context.Context, userID, bookID int64, overall float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reading_progress (user_id, book_id, chapter, position, overall, locator, updated_at)
		VALUES (?, ?, 0, 0, ?, '', strftime('%Y-%m-%d %H:%M:%f', 'now'))
		ON CONFLICT (user_id, book_id) DO UPDATE
		SET overall = excluded.overall, updated_at = excluded.updated_at`,
		userID, bookID, overall)
	return err
}
