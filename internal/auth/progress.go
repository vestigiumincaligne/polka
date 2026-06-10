package auth

import (
	"context"
	"database/sql"
	"errors"
)

// Reading progress lives in users.db: it is personal and must
// survive collection re-import.

const progressSchema = `
CREATE TABLE IF NOT EXISTS reading_progress (
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	book_id    INTEGER NOT NULL,
	chapter    INTEGER NOT NULL DEFAULT 0,
	position   REAL NOT NULL DEFAULT 0,
	overall    REAL NOT NULL DEFAULT 0,
	locator    TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (user_id, book_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_progress_updated ON reading_progress (user_id, updated_at);
`

// migrateProgress upgrades the table from early builds: adds the overall
// column; if it already exists, ALTER fails with duplicate column — ignored.
func migrateProgress(db *sql.DB) {
	db.Exec(`ALTER TABLE reading_progress ADD COLUMN overall REAL NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE reading_progress ADD COLUMN locator TEXT NOT NULL DEFAULT ''`)
}

type Progress struct {
	BookID   int64
	Chapter  int
	Position float64 // scroll fraction of the chapter, 0..1
	Overall  float64 // fraction of the whole book, 0..1
	Locator  string  // format-specific position (CFI for epub, etc.)
}

func (s *Service) SaveProgress(ctx context.Context, userID, bookID int64, p Progress) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO reading_progress (user_id, book_id, chapter, position, overall, locator, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT (user_id, book_id) DO UPDATE
		SET chapter = excluded.chapter, position = excluded.position,
		    overall = excluded.overall, locator = excluded.locator, updated_at = excluded.updated_at`,
		userID, bookID, p.Chapter, p.Position, p.Overall, p.Locator)
	return err
}

func (s *Service) GetProgress(ctx context.Context, userID, bookID int64) (Progress, error) {
	p := Progress{BookID: bookID}
	err := s.db.QueryRowContext(ctx,
		`SELECT chapter, position, overall, locator FROM reading_progress WHERE user_id = ? AND book_id = ?`,
		userID, bookID).Scan(&p.Chapter, &p.Position, &p.Overall, &p.Locator)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ListProgress returns the user's recently read books (newest first).
// Finished books (overall ~1) are not shown.
func (s *Service) ListProgress(ctx context.Context, userID int64, limit int) ([]Progress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id, chapter, position, overall FROM reading_progress
		WHERE user_id = ? AND overall < 0.98
		ORDER BY updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Progress
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.BookID, &p.Chapter, &p.Position, &p.Overall); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
