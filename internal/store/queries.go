package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Book is a book-list row in the format expected by the frontend.
type Book struct {
	ID          int64
	Title       string
	AuthorNames string
	SeriesTitle string
	SeqNumber   int
	Year        int
	LibRate     float64
	Ext         string
	Size        int64
}

// BookFile holds the data needed to serve a book file.
type BookFile struct {
	ID     int64
	Title  string
	Folder string
	File   string
	Ext    string
	Size   int64
}

// BookDetails is the full book card.
type BookDetails struct {
	Book
	File    string
	Folder  string
	Authors []AuthorName
	Series  []SeriesRef
	Genres  []string
}

type SeriesRef struct {
	ID        int64
	Title     string
	SeqNumber int
}

type AuthorEntry struct {
	ID    int64
	Name  string
	Books int
}

type SeriesEntry struct {
	ID    int64
	Title string
	Books int
}

var ErrNotFound = errors.New("not found")

const bookColumns = `
	b.id,
	b.title,
	coalesce((SELECT group_concat(trim(a.last_name || ' ' || a.first_name), ', ')
	          FROM book_authors ba JOIN authors a ON a.id = ba.author_id
	          WHERE ba.book_id = b.id), ''),
	coalesce(s.title, ''),
	coalesce(b.series_num, 0),
	coalesce(b.year, 0),
	b.lib_rate,
	b.ext,
	b.size`

const bookFrom = ` FROM books b LEFT JOIN series s ON s.id = b.series_id`

func (s *Store) queryBooks(ctx context.Context, where string, order string, limit, offset int, args ...any) ([]Book, error) {
	q := `SELECT` + bookColumns + bookFrom + ` WHERE b.deleted = 0`
	if where != "" {
		q += ` AND ` + where
	}
	if order != "" {
		q += ` ORDER BY ` + order
	}
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d OFFSET %d`, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(&b.ID, &b.Title, &b.AuthorNames, &b.SeriesTitle,
			&b.SeqNumber, &b.Year, &b.LibRate, &b.Ext, &b.Size); err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	return books, rows.Err()
}

// ftsQuery turns user input into a safe prefix FTS5 expression:
// each token becomes "token"* , terms are joined with AND.
func ftsQuery(input, column string) string {
	var terms []string
	for _, tok := range strings.FieldsFunc(input, func(r rune) bool {
		return !isWordRune(r)
	}) {
		tok = strings.ReplaceAll(tok, `"`, "")
		if tok == "" {
			continue
		}
		term := `"` + tok + `"*`
		if column != "" {
			term = column + `:` + term
		}
		// unicode61 does not equate ё and е: "Вишнёвый" would not find
		// "Вишневый", so we search for both forms.
		if alt := strings.NewReplacer("ё", "е", "Ё", "Е").Replace(tok); alt != tok {
			altTerm := `"` + alt + `"*`
			if column != "" {
				altTerm = column + `:` + altTerm
			}
			term = `(` + term + ` OR ` + altTerm + `)`
		}
		terms = append(terms, term)
	}
	// Explicit AND: FTS5 does not accept implicit joining after a parenthesized group.
	return strings.Join(terms, " AND ")
}

func isWordRune(r rune) bool {
	return r == '-' || r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		r > 127
}

// --- Search ---

type SearchStats struct {
	BookTitles, Authors, BookSeries int
}

func (s *Store) SearchStats(ctx context.Context, query string) (SearchStats, error) {
	var st SearchStats
	titleQ, plainQ := ftsQuery(query, "title"), ftsQuery(query, "")
	if plainQ == "" {
		return st, nil
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM book_search WHERE book_search MATCH ?`, titleQ).Scan(&st.BookTitles); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM authors_search WHERE authors_search MATCH ?`, plainQ).Scan(&st.Authors); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM series_search WHERE series_search MATCH ?`, plainQ).Scan(&st.BookSeries); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Store) SearchTitles(ctx context.Context, query string, limit int) ([]Book, error) {
	q := ftsQuery(query, "title")
	if q == "" {
		return nil, nil
	}
	// For selective queries, pick the best matches by bm25 rank.
	// If the query matched a huge share of the library, ranking hundreds
	// of thousands of rows costs more than it's worth — take the first matches found.
	var matches int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM book_search WHERE book_search MATCH ?`, q).Scan(&matches); err != nil {
		return nil, err
	}
	sub := `SELECT rowid FROM book_search WHERE book_search MATCH ? ORDER BY rank LIMIT ?`
	if matches > 5000 {
		sub = `SELECT rowid FROM book_search WHERE book_search MATCH ? LIMIT ?`
	}
	return s.queryBooks(ctx, `b.id IN (`+sub+`)`, `b.title`, 0, 0, q, limit)
}

func (s *Store) SearchAuthors(ctx context.Context, query string, limit int) ([]AuthorEntry, error) {
	q := ftsQuery(query, "")
	if q == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id,
		       trim(a.last_name || ' ' || a.first_name || ' ' || a.middle_name),
		       (SELECT count(*) FROM book_authors ba JOIN books b ON b.id = ba.book_id
		        WHERE ba.author_id = a.id AND b.deleted = 0)
		FROM authors a
		WHERE a.id IN (SELECT rowid FROM authors_search WHERE authors_search MATCH ?)
		ORDER BY a.last_name, a.first_name
		LIMIT ?`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuthorEntry
	for rows.Next() {
		var e AuthorEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Books); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SearchSeries(ctx context.Context, query string, limit int) ([]SeriesEntry, error) {
	q := ftsQuery(query, "")
	if q == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.title,
		       (SELECT count(*) FROM books b WHERE b.series_id = s.id AND b.deleted = 0)
		FROM series s
		WHERE s.id IN (SELECT rowid FROM series_search WHERE series_search MATCH ?)
		ORDER BY s.title
		LIMIT ?`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SeriesEntry
	for rows.Next() {
		var e SeriesEntry
		if err := rows.Scan(&e.ID, &e.Title, &e.Books); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AuthorBooks(ctx context.Context, authorID int64) ([]Book, string, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT trim(last_name || ' ' || first_name || ' ' || middle_name) FROM authors WHERE id = ?`,
		authorID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	books, err := s.queryBooks(ctx,
		`b.id IN (SELECT book_id FROM book_authors WHERE author_id = ?)`,
		`coalesce(s.title, ''), b.series_num, b.title`, 0, 0, authorID)
	return books, name, err
}

func (s *Store) SeriesBooks(ctx context.Context, seriesID int64) ([]Book, string, error) {
	var title string
	err := s.db.QueryRowContext(ctx, `SELECT title FROM series WHERE id = ?`, seriesID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	books, err := s.queryBooks(ctx, `b.series_id = ?`, `b.series_num, b.title`, 0, 0, seriesID)
	return books, title, err
}

// --- Shelves ---

type Shelf struct {
	ID      string
	Title   string
	Books   []Book
	HasMore bool
}

// shelfWhere returns the condition and ordering for built-in shelves.
func shelfWhere(shelfID string) (where, order string, ok bool) {
	switch shelfID {
	case "newest":
		return ``, `b.added DESC, b.id DESC`, true
	case "top_rated":
		return `b.lib_rate >= 4`, `b.lib_rate DESC, b.id DESC`, true
	case "well_curated":
		return `b.lib_rate BETWEEN 1 AND 3`, `b.lib_rate DESC, b.id DESC`, true
	}
	return "", "", false
}

func (s *Store) HomeShelves(ctx context.Context, limit int) ([]Shelf, error) {
	var shelves []Shelf
	for _, id := range []string{"newest", "top_rated", "well_curated"} {
		books, _, err := s.ShelfBooks(ctx, id, limit, 0)
		if err != nil {
			return nil, err
		}
		shelves = append(shelves, Shelf{ID: id, Books: books, HasMore: len(books) == limit})
	}
	return shelves, nil
}

// ShelfBooks returns a page of a built-in ("newest"...) or genre
// ("genre_<code>") shelf.
func (s *Store) ShelfBooks(ctx context.Context, shelfID string, limit, offset int) ([]Book, string, error) {
	if code, isGenre := strings.CutPrefix(shelfID, "genre_"); isGenre {
		var genreID int64
		var title string
		err := s.db.QueryRowContext(ctx,
			`SELECT id, coalesce(nullif(name, ''), code) FROM genres WHERE code = ?`, code).Scan(&genreID, &title)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		if err != nil {
			return nil, "", err
		}
		books, err := s.queryBooks(ctx,
			`b.id IN (SELECT book_id FROM book_genres WHERE genre_id = ?)`,
			`b.id`, limit, offset, genreID)
		return books, title, err
	}

	where, order, ok := shelfWhere(shelfID)
	if !ok {
		return nil, "", ErrNotFound
	}
	books, err := s.queryBooks(ctx, where, order, limit, offset)
	return books, "", err
}

// CatalogShelves returns random genre selections for the catalog page.
func (s *Store) CatalogShelves(ctx context.Context, count, limit int) ([]Shelf, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.code, coalesce(nullif(g.name, ''), g.code)
		FROM genres g
		WHERE EXISTS (SELECT 1 FROM book_genres bg WHERE bg.genre_id = g.id)
		ORDER BY random()
		LIMIT ?`, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type genre struct {
		id    int64
		code  string
		title string
	}
	var picked []genre
	for rows.Next() {
		var g genre
		if err := rows.Scan(&g.id, &g.code, &g.title); err != nil {
			return nil, err
		}
		picked = append(picked, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var shelves []Shelf
	for _, g := range picked {
		books, err := s.queryBooks(ctx,
			`b.id IN (SELECT bg.book_id FROM book_genres bg JOIN books lb ON lb.id = bg.book_id AND lb.deleted = 0
			          WHERE bg.genre_id = ? ORDER BY random() LIMIT ?)`,
			`b.title`, 0, 0, g.id, limit)
		if err != nil {
			return nil, err
		}
		if len(books) == 0 {
			continue
		}
		shelves = append(shelves, Shelf{
			ID:      "genre_" + g.code,
			Title:   g.title,
			Books:   books,
			HasMore: len(books) == limit,
		})
	}
	return shelves, nil
}

// --- OPDS ---

type GenreCount struct {
	Code  string
	Books int
}

// GenresWithCounts returns genres that have live (non-deleted) books.
func (s *Store) GenresWithCounts(ctx context.Context) ([]GenreCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.code, count(*)
		FROM genres g
		JOIN book_genres bg ON bg.genre_id = g.id
		JOIN books b ON b.id = bg.book_id AND b.deleted = 0
		GROUP BY g.id
		HAVING count(*) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GenreCount
	for rows.Next() {
		var g GenreCount
		if err := rows.Scan(&g.Code, &g.Books); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SearchBooks searches across all fields (title, authors, series)
// with pagination, for the OPDS catalog.
func (s *Store) SearchBooks(ctx context.Context, query string, limit, offset int) ([]Book, error) {
	q := ftsQuery(query, "")
	if q == "" {
		return nil, nil
	}
	return s.queryBooks(ctx,
		`b.id IN (SELECT rowid FROM book_search WHERE book_search MATCH ? LIMIT ? OFFSET ?)`,
		`b.title`, 0, 0, q, limit, offset)
}

// --- Book card and book file ---

func (s *Store) BookDetails(ctx context.Context, bookID int64) (*BookDetails, error) {
	d := &BookDetails{}
	err := s.db.QueryRowContext(ctx,
		`SELECT`+bookColumns+`, b.file, f.name`+bookFrom+
			` JOIN folders f ON f.id = b.folder_id WHERE b.id = ? AND b.deleted = 0`, bookID).
		Scan(&d.ID, &d.Title, &d.AuthorNames, &d.SeriesTitle, &d.SeqNumber,
			&d.Year, &d.LibRate, &d.Ext, &d.Size, &d.File, &d.Folder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.last_name, a.first_name, a.middle_name
		FROM book_authors ba JOIN authors a ON a.id = ba.author_id
		WHERE ba.book_id = ? ORDER BY a.last_name`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AuthorName
		if err := rows.Scan(&a.ID, &a.Last, &a.First, &a.Middle); err != nil {
			return nil, err
		}
		d.Authors = append(d.Authors, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if d.SeriesTitle != "" {
		var sid int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT coalesce(series_id, 0) FROM books WHERE id = ?`, bookID).Scan(&sid); err == nil && sid > 0 {
			d.Series = append(d.Series, SeriesRef{ID: sid, Title: d.SeriesTitle, SeqNumber: d.SeqNumber})
		}
	}

	grows, err := s.db.QueryContext(ctx, `
		SELECT g.code FROM book_genres bg JOIN genres g ON g.id = bg.genre_id
		WHERE bg.book_id = ?`, bookID)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var code string
		if err := grows.Scan(&code); err != nil {
			return nil, err
		}
		d.Genres = append(d.Genres, code)
	}
	return d, grows.Err()
}

// SampleFolders returns up to limit catalog folder (archive) names, to check
// after an import that the archives actually exist on disk.
func (s *Store) SampleFolders(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM folders
		WHERE id IN (SELECT folder_id FROM books WHERE deleted = 0)
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) BookFile(ctx context.Context, bookID int64) (*BookFile, error) {
	f := &BookFile{}
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.title, fo.name, b.file, b.ext, b.size
		FROM books b JOIN folders fo ON fo.id = b.folder_id
		WHERE b.id = ? AND b.deleted = 0`, bookID).
		Scan(&f.ID, &f.Title, &f.Folder, &f.File, &f.Ext, &f.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

// AuthorsByPrefix lists authors whose last name falls in [lo, hi),
// ordered by name — for alphabetical OPDS browsing. Uses idx_authors_name.
func (s *Store) AuthorsByPrefix(ctx context.Context, lo, hi string, limit, offset int) ([]AuthorEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id,
		       trim(a.last_name || ' ' || a.first_name || ' ' || a.middle_name),
		       (SELECT count(*) FROM book_authors ba JOIN books b ON b.id = ba.book_id
		        WHERE ba.author_id = a.id AND b.deleted = 0)
		FROM authors a
		WHERE a.last_name >= ? AND a.last_name < ?
		ORDER BY a.last_name, a.first_name
		LIMIT ? OFFSET ?`, lo, hi, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthorEntry
	for rows.Next() {
		var e AuthorEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Books); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SeriesByPrefix lists series whose title falls in [lo, hi), ordered by title.
func (s *Store) SeriesByPrefix(ctx context.Context, lo, hi string, limit, offset int) ([]SeriesEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.title,
		       (SELECT count(*) FROM books b WHERE b.series_id = s.id AND b.deleted = 0)
		FROM series s
		WHERE s.title >= ? AND s.title < ?
		ORDER BY s.title
		LIMIT ? OFFSET ?`, lo, hi, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesEntry
	for rows.Next() {
		var e SeriesEntry
		if err := rows.Scan(&e.ID, &e.Title, &e.Books); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
