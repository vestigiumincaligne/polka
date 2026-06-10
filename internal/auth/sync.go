package auth

import (
	"context"
)

// User data state for syncing the desktop client with the server:
// timestamped snapshots, last-write-wins merge.

type ProgressState struct {
	BookID    int64   `json:"bookId"`
	Chapter   int     `json:"chapter"`
	Position  float64 `json:"position"`
	Overall   float64 `json:"overall"`
	Locator   string  `json:"locator"`
	UpdatedAt string  `json:"updatedAt"` // UTC, SQLite datetime format
}

type RatingState struct {
	BookID    int64  `json:"bookId"`
	Rating    int    `json:"rating"`
	UpdatedAt string `json:"updatedAt"`
}

type ListState struct {
	Name    string          `json:"name"`
	Builtin string          `json:"builtin"`
	Books   []ListBookState `json:"books"`
}

type ListBookState struct {
	BookID  int64  `json:"bookId"`
	AddedAt string `json:"addedAt"`
}

type SyncState struct {
	Progress []ProgressState `json:"progress"`
	Ratings  []RatingState   `json:"ratings"`
	Lists    []ListState     `json:"lists"`
}

// ExportState collects all user data with timestamps.
func (s *Service) ExportState(ctx context.Context, userID int64) (*SyncState, error) {
	state := &SyncState{}

	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id, chapter, position, overall, locator, updated_at
		FROM reading_progress WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p ProgressState
		if err := rows.Scan(&p.BookID, &p.Chapter, &p.Position, &p.Overall, &p.Locator, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		state.Progress = append(state.Progress, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT book_id, rating, updated_at FROM book_ratings WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r RatingState
		if err := rows.Scan(&r.BookID, &r.Rating, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		state.Ratings = append(state.Ratings, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lists, err := s.Lists(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, l := range lists {
		ls := ListState{Name: l.Name, Builtin: l.Builtin}
		brows, err := s.db.QueryContext(ctx,
			`SELECT book_id, added_at FROM list_books WHERE list_id = ?`, l.ID)
		if err != nil {
			return nil, err
		}
		for brows.Next() {
			var b ListBookState
			if err := brows.Scan(&b.BookID, &b.AddedAt); err != nil {
				brows.Close()
				return nil, err
			}
			ls.Books = append(ls.Books, b)
		}
		brows.Close()
		if err := brows.Err(); err != nil {
			return nil, err
		}
		state.Lists = append(state.Lists, ls)
	}
	return state, nil
}

// MergeState merges foreign state: progress and ratings — last-write-wins
// by updated_at, lists — union (create missing ones, add books).
func (s *Service) MergeState(ctx context.Context, userID int64, in *SyncState) error {
	for _, p := range in.Progress {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO reading_progress (user_id, book_id, chapter, position, overall, locator, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, book_id) DO UPDATE SET
				chapter = excluded.chapter, position = excluded.position,
				overall = excluded.overall, locator = excluded.locator,
				updated_at = excluded.updated_at
			WHERE excluded.updated_at > reading_progress.updated_at`,
			userID, p.BookID, p.Chapter, p.Position, p.Overall, p.Locator, p.UpdatedAt); err != nil {
			return err
		}
	}

	for _, r := range in.Ratings {
		if r.Rating < 1 || r.Rating > 5 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO book_ratings (user_id, book_id, rating, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (user_id, book_id) DO UPDATE SET
				rating = excluded.rating, updated_at = excluded.updated_at
			WHERE excluded.updated_at > book_ratings.updated_at`,
			userID, r.BookID, r.Rating, r.UpdatedAt); err != nil {
			return err
		}
	}

	for _, ls := range in.Lists {
		var listID int64
		if ls.Builtin == BuiltinWishlist {
			wl, err := s.Wishlist(ctx, userID)
			if err != nil {
				return err
			}
			listID = wl.ID
		} else {
			err := s.db.QueryRowContext(ctx,
				`SELECT id FROM lists WHERE user_id = ? AND name = ?`, userID, ls.Name).Scan(&listID)
			if err != nil {
				created, cerr := s.CreateList(ctx, userID, ls.Name)
				if cerr != nil {
					return cerr
				}
				listID = created.ID
			}
		}
		for _, b := range ls.Books {
			if _, err := s.db.ExecContext(ctx, `
				INSERT OR IGNORE INTO list_books (list_id, book_id, added_at) VALUES (?, ?, ?)`,
				listID, b.BookID, b.AddedAt); err != nil {
				return err
			}
		}
	}
	return nil
}
