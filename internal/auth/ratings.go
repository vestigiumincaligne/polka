package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Server settings and book ratings live in users.db —
// they must survive collection re-import.

const settingsSchema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS book_ratings (
	user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	book_id    INTEGER NOT NULL,
	rating     INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (user_id, book_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_ratings_book ON book_ratings (book_id);
`

// --- Settings (key-value) ---

func (s *Service) GetSetting(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Service) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// --- Book ratings ---

// RateBook sets a rating 1..5; rating = 0 removes the rating.
func (s *Service) RateBook(ctx context.Context, userID, bookID int64, rating int) error {
	if rating == 0 {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM book_ratings WHERE user_id = ? AND book_id = ?`, userID, bookID)
		return err
	}
	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be 0..5")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO book_ratings (user_id, book_id, rating, updated_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
		ON CONFLICT (user_id, book_id) DO UPDATE
		SET rating = excluded.rating, updated_at = excluded.updated_at`,
		userID, bookID, rating)
	return err
}

// BookRating returns the book's average rating across all users.
func (s *Service) BookRating(ctx context.Context, bookID int64) (avg float64, count int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT coalesce(avg(rating), 0), count(*) FROM book_ratings WHERE book_id = ?`, bookID).
		Scan(&avg, &count)
	return avg, count, err
}

// RatedBookIDs returns all books rated by the user; minRating
// filters "liked" ones (0 — any).
func (s *Service) RatedBookIDs(ctx context.Context, userID int64, minRating int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT book_id FROM book_ratings WHERE user_id = ? AND rating >= ? ORDER BY updated_at DESC`,
		userID, minRating)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AllListBookIDs returns books across all of the user's lists.
func (s *Service) AllListBookIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT lb.book_id FROM list_books lb
		JOIN lists l ON l.id = lb.list_id WHERE l.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UserRating returns the book's rating by a specific user (0 — not rated).
func (s *Service) UserRating(ctx context.Context, userID, bookID int64) int {
	var r int
	err := s.db.QueryRowContext(ctx,
		`SELECT rating FROM book_ratings WHERE user_id = ? AND book_id = ?`, userID, bookID).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return 0
	}
	return r
}
