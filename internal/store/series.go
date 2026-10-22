package store

import (
	"context"
	"database/sql"
	"strings"
)

// seriesRefs keeps the legacy primary series first and removes duplicate names.
func (b *BookInput) seriesRefs() []SeriesRef {
	var refs []SeriesRef
	if b.Series != "" {
		refs = append(refs, SeriesRef{Title: b.Series, SeqNumber: b.SeriesNum})
	}
	refs = append(refs, b.SeriesList...)
	var out []SeriesRef
	seen := make(map[string]int)
	for _, ref := range refs {
		ref.Title = strings.TrimSpace(ref.Title)
		if ref.Title == "" {
			continue
		}
		ref.SeqNumber = max(0, ref.SeqNumber)
		if index, ok := seen[ref.Title]; ok {
			if out[index].SeqNumber == 0 {
				out[index].SeqNumber = ref.SeqNumber
			}
			continue
		}
		seen[ref.Title] = len(out)
		out = append(out, ref)
	}
	return out
}

func seriesTitles(refs []SeriesRef) string {
	titles := make([]string, 0, len(refs))
	for _, ref := range refs {
		titles = append(titles, ref.Title)
	}
	return strings.Join(titles, " ")
}

func (s *Store) BookSeries(ctx context.Context, bookID int64) ([]SeriesRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.title, bs.series_num
		FROM book_series bs JOIN series s ON s.id = bs.series_id
		WHERE bs.book_id = ? ORDER BY bs.position, s.id`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []SeriesRef
	for rows.Next() {
		var ref SeriesRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.SeqNumber); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func addBookSeries(ctx context.Context, tx *sql.Tx, bookID int64, refs []SeriesRef) (bool, error) {
	changed := false
	for _, ref := range refs {
		id, err := lookupOrInsert(ctx, tx,
			`SELECT id FROM series WHERE title = ?`, `INSERT INTO series (title) VALUES (?)`,
			func(id int64) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO series_search (rowid, title) VALUES (?, ?)`, id, ref.Title)
				return err
			}, ref.Title)
		if err != nil {
			return false, err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO book_series (book_id, series_id, series_num, position)
			VALUES (?, ?, ?, (SELECT coalesce(max(position) + 1, 0) FROM book_series WHERE book_id = ?))
			ON CONFLICT (book_id, series_id) DO UPDATE SET series_num = excluded.series_num
			WHERE excluded.series_num > 0 AND book_series.series_num != excluded.series_num`,
			bookID, id, ref.SeqNumber, bookID)
		if err != nil {
			return false, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, err
		} else if n > 0 {
			changed = true
		}
	}
	return changed, nil
}

// AddBookSeries lazily supplements catalog metadata from an existing FB2 file.
// Missing numbers never erase the sequence numbers already in the catalog.
func (s *Store) AddBookSeries(ctx context.Context, bookID int64, refs []SeriesRef) error {
	refs = (&BookInput{SeriesList: refs}).seriesRefs()
	if len(refs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	changed, err := addBookSeries(ctx, tx, bookID, refs)
	if err != nil {
		return err
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE books SET
				series_id = coalesce(series_id, (SELECT series_id FROM book_series WHERE book_id = books.id ORDER BY position LIMIT 1)),
				series_num = (SELECT series_num FROM book_series WHERE book_id = books.id
				              AND (books.series_id IS NULL OR series_id = books.series_id) ORDER BY position LIMIT 1)
			WHERE id = ?`, bookID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE book_search SET series = (
				SELECT group_concat(s.title, ' ') FROM book_series bs JOIN series s ON s.id = bs.series_id
				WHERE bs.book_id = ?
			) WHERE rowid = ?`, bookID, bookID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
