package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// lookupOrInsert returns the row id for a unique value, creating the row
// if needed. afterInsert is called for newly created rows (FTS etc.).
func lookupOrInsert(ctx context.Context, tx *sql.Tx, selectQ, insertQ string, afterInsert func(id int64) error, args ...any) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, selectQ, args...).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, insertQ, args...)
	if err != nil {
		return 0, err
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	if afterInsert != nil {
		if err := afterInsert(id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// AddBook adds a single book (web upload) with all its relations
// and updates the search indexes.
func (s *Store) AddBook(ctx context.Context, b *BookInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	folderID, err := lookupOrInsert(ctx, tx,
		`SELECT id FROM folders WHERE name = ?`, `INSERT INTO folders (name) VALUES (?)`, nil, b.Folder)
	if err != nil {
		return 0, err
	}

	var seriesID any
	if b.Series != "" {
		id, err := lookupOrInsert(ctx, tx,
			`SELECT id FROM series WHERE title = ?`, `INSERT INTO series (title) VALUES (?)`,
			func(id int64) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO series_search (rowid, title) VALUES (?, ?)`, id, b.Series)
				return err
			}, b.Series)
		if err != nil {
			return 0, err
		}
		seriesID = id
	}
	var seriesNum any
	if b.SeriesNum > 0 {
		seriesNum = b.SeriesNum
	}
	var year any
	if b.Year > 0 {
		year = b.Year
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO books (lib_id, title, series_id, series_num, folder_id, file, ext, size, lang, year, added, lib_rate, deleted, file_hash, content_hash, isbn)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		b.LibID, b.Title, seriesID, seriesNum, folderID, b.File, b.Ext, b.Size, b.Lang, year, b.Added, b.Rate, b.FileHash, b.ContentHash, NormalizeISBN(b.ISBN))
	if err != nil {
		return 0, err
	}
	bookID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	var authorNames []string
	for _, a := range b.Authors {
		name := strings.TrimSpace(a.Last + " " + a.First + " " + a.Middle)
		authorNames = append(authorNames, name)
		id, err := lookupOrInsert(ctx, tx,
			`SELECT id FROM authors WHERE last_name = ? AND first_name = ? AND middle_name = ?`,
			`INSERT INTO authors (last_name, first_name, middle_name) VALUES (?, ?, ?)`,
			func(id int64) error {
				_, err := tx.ExecContext(ctx, `INSERT INTO authors_search (rowid, name) VALUES (?, ?)`, id, name)
				return err
			}, a.Last, a.First, a.Middle)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO book_authors (book_id, author_id) VALUES (?, ?)`, bookID, id); err != nil {
			return 0, err
		}
	}
	for _, g := range b.Genres {
		id, err := lookupOrInsert(ctx, tx,
			`SELECT id FROM genres WHERE code = ?`, `INSERT INTO genres (code) VALUES (?)`, nil, g)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO book_genres (book_id, genre_id) VALUES (?, ?)`, bookID, id); err != nil {
			return 0, err
		}
	}
	for _, k := range b.Keywords {
		id, err := lookupOrInsert(ctx, tx,
			`SELECT id FROM keywords WHERE name = ?`, `INSERT INTO keywords (name) VALUES (?)`, nil, k)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO book_keywords (book_id, keyword_id) VALUES (?, ?)`, bookID, id); err != nil {
			return 0, err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO book_search (rowid, title, authors, series) VALUES (?, ?, ?, ?)`,
		bookID, b.Title, strings.Join(authorNames, " "), b.Series); err != nil {
		return 0, err
	}

	return bookID, tx.Commit()
}

// SetBookDeleted hides or restores a book, keeping the FTS index in sync.
func (s *Store) SetBookDeleted(ctx context.Context, bookID int64, deleted bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE books SET deleted = ? WHERE id = ?`, boolToInt(deleted), bookID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	if deleted {
		_, err = s.db.ExecContext(ctx, `DELETE FROM book_search WHERE rowid = ?`, bookID)
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO book_search (rowid, title, authors, series)
		SELECT b.id, b.title,
		       coalesce((SELECT group_concat(trim(a.last_name || ' ' || a.first_name), ' ')
		                 FROM book_authors ba JOIN authors a ON a.id = ba.author_id
		                 WHERE ba.book_id = b.id), ''),
		       coalesce(s.title, '')
		FROM books b LEFT JOIN series s ON s.id = b.series_id WHERE b.id = ?`, bookID)
	return err
}

// Clear empties the collection (for re-import on a running server).
// The user database and meta are not touched.
func (s *Store) Clear(ctx context.Context) error {
	tables := []string{
		"book_search", "authors_search", "series_search",
		"book_authors", "book_genres", "book_keywords",
		"books", "authors", "series", "genres", "keywords", "folders",
	}
	for _, table := range tables {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

// FindByHash looks up a live book by an exact match of the file hash or
// the normalized-text hash.
func (s *Store) FindByHash(ctx context.Context, column, hash string) (*Book, error) {
	if hash == "" {
		return nil, ErrNotFound
	}
	if column != "file_hash" && column != "content_hash" {
		return nil, fmt.Errorf("bad hash column %q", column)
	}
	books, err := s.queryBooks(ctx, `b.`+column+` = ?`, "", 1, 0, hash)
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, ErrNotFound
	}
	return &books[0], nil
}

// normalizeForMatch normalizes a string for metadata comparison:
// letters and digits only, lowercase, Cyrillic yo collapsed to ye.
func normalizeForMatch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == 'ё' {
			r = 'е'
		}
		if isWordRune(r) && r != '-' && r != '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FindSimilar finds books with the same normalized title and at least
// one shared author (by last name) — candidates for re-edition duplicates.
func (s *Store) FindSimilar(ctx context.Context, title string, authorLastNames []string) ([]Book, error) {
	q := ftsQuery(title, "title")
	if q == "" {
		return nil, nil
	}
	candidates, err := s.queryBooks(ctx,
		`b.id IN (SELECT rowid FROM book_search WHERE book_search MATCH ? LIMIT 50)`,
		"", 0, 0, q)
	if err != nil {
		return nil, err
	}

	wantTitle := normalizeForMatch(title)
	wantAuthors := make(map[string]bool, len(authorLastNames))
	for _, a := range authorLastNames {
		if n := normalizeForMatch(a); n != "" {
			wantAuthors[n] = true
		}
	}

	var similar []Book
	for _, c := range candidates {
		if normalizeForMatch(c.Title) != wantTitle {
			continue
		}
		if len(wantAuthors) == 0 {
			similar = append(similar, c)
			continue
		}
		for _, name := range strings.Split(c.AuthorNames, ", ") {
			last := strings.Fields(name)
			if len(last) > 0 && wantAuthors[normalizeForMatch(last[0])] {
				similar = append(similar, c)
				break
			}
		}
	}
	return similar, nil
}

// BooksByIDs returns books in the order of the given ids (missing
// and deleted ones are skipped).
func (s *Store) BooksByIDs(ctx context.Context, ids []int64) ([]Book, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	books, err := s.queryBooks(ctx, `b.id IN (`+placeholders+`)`, "", 0, 0, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Book, len(books))
	for _, b := range books {
		byID[b.ID] = b
	}
	ordered := make([]Book, 0, len(books))
	for _, id := range ids {
		if b, ok := byID[id]; ok {
			ordered = append(ordered, b)
		}
	}
	return ordered, nil
}

// BookFilesByIDs returns files of existing books for bulk serving.
func (s *Store) BookFilesByIDs(ctx context.Context, ids []int64) ([]BookFile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.title, fo.name, b.file, b.ext, b.size
		FROM books b JOIN folders fo ON fo.id = b.folder_id
		WHERE b.deleted = 0 AND b.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []BookFile
	for rows.Next() {
		var f BookFile
		if err := rows.Scan(&f.ID, &f.Title, &f.Folder, &f.File, &f.Ext, &f.Size); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
