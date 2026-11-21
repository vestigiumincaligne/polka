package auth

import (
	"context"
	"database/sql"
	"errors"
)

// BuiltinRead is the built-in "Read" list. A book gets there by the button on
// the book page (for books read outside Polka) or automatically when the
// reading progress first reaches ReadThreshold. Marking a book read removes
// it from the "Want to read" list.
const BuiltinRead = "read"

// ReadThreshold is the overall progress at which a book counts as read.
const ReadThreshold = 0.97

const readListName = "Прочитано"

// builtinList returns the user's built-in list of the given kind, creating it.
func (s *Service) builtinList(ctx context.Context, userID int64, builtin, name string) (*List, error) {
	var l List
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, builtin FROM lists WHERE user_id = ? AND builtin = ?`,
		userID, builtin).Scan(&l.ID, &l.Name, &l.Builtin)
	if err == nil {
		return &l, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// A user list may already carry the name: take a free variant.
	for _, candidate := range []string{name, name + " ✓", name + " (Polka)"} {
		res, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO lists (user_id, name, builtin) VALUES (?, ?, ?)`, userID, candidate, builtin)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			id, _ := res.LastInsertId()
			return &List{ID: id, Name: candidate, Builtin: builtin}, nil
		}
	}
	return nil, ErrListNameTaken
}

// ReadList returns the user's built-in "Read" list, creating it if needed.
func (s *Service) ReadList(ctx context.Context, userID int64) (*List, error) {
	return s.builtinList(ctx, userID, BuiltinRead, readListName)
}

// SetRead marks or unmarks a book as read. Marking also drops it from the
// "Want to read" list.
func (s *Service) SetRead(ctx context.Context, userID, bookID int64, read bool) (*List, error) {
	rl, err := s.ReadList(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !read {
		_, err = s.db.ExecContext(ctx, `DELETE FROM list_books WHERE list_id = ? AND book_id = ?`, rl.ID, bookID)
		return rl, err
	}
	if _, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO list_books (list_id, book_id) VALUES (?, ?)`, rl.ID, bookID); err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM list_books WHERE book_id = ? AND list_id IN
			(SELECT id FROM lists WHERE user_id = ? AND builtin = ?)`, bookID, userID, BuiltinWishlist)
	return rl, err
}

// ReadBookIDs returns every book the user marked read (for card badges).
func (s *Service) ReadBookIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT lb.book_id FROM list_books lb JOIN lists l ON l.id = lb.list_id
		WHERE l.user_id = ? AND l.builtin = ?`, userID, BuiltinRead)
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

// previousOverall returns the stored overall progress (0 if none).
func (s *Service) previousOverall(ctx context.Context, userID, bookID int64) float64 {
	var overall float64
	s.db.QueryRowContext(ctx,
		`SELECT overall FROM reading_progress WHERE user_id = ? AND book_id = ?`, userID, bookID).Scan(&overall)
	return overall
}

// markReadOnCrossing marks the book read when the progress has just reached
// the threshold. Only the crossing counts, so a book the user unmarked is
// not re-marked by every later save near the end.
func (s *Service) markReadOnCrossing(ctx context.Context, userID, bookID int64, before, after float64) error {
	if before < ReadThreshold && after >= ReadThreshold {
		_, err := s.SetRead(ctx, userID, bookID, true)
		return err
	}
	return nil
}

// backfillRead runs once per database: books whose stored progress already
// reached the threshold before the "Read" list existed are marked read.
func (s *Service) backfillRead(ctx context.Context) error {
	const key = "read_backfill_v1"
	if s.GetSetting(ctx, key, "") != "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, book_id FROM reading_progress WHERE overall >= ?`, ReadThreshold)
	if err != nil {
		return err
	}
	type pair struct{ user, book int64 }
	var done []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.user, &p.book); err != nil {
			rows.Close()
			return err
		}
		done = append(done, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range done {
		if _, err := s.SetRead(ctx, p.user, p.book, true); err != nil {
			return err
		}
	}
	return s.SetSetting(ctx, key, "done")
}
