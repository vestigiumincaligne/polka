package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// BookInput is a single book for bulk import.
type BookInput struct {
	LibID     string
	Title     string
	Series    string
	SeriesNum int
	Authors   []AuthorName
	Genres    []string
	Keywords  []string
	Folder    string
	File      string
	Ext       string
	Size      int64
	Lang      string
	Year      int
	Added     string
	Rate      float64
	Deleted   bool
	// Hashes for duplicate detection (populated on web upload).
	FileHash    string
	ContentHash string
	ISBN        string
}

type AuthorName struct {
	ID                  int64
	Last, First, Middle string
}

type ImportStats struct {
	Books    int
	Authors  int
	Series   int
	Genres   int
	Duration time.Duration
}

// ImportSession loads books in batches: inserts run in large transactions
// without indexes; the indexes and the FTS search index are built once in Finish.
type ImportSession struct {
	s     *Store
	ctx   context.Context
	tx    *sql.Tx
	stmts importStmts
	batch int
	start time.Time

	folders  map[string]int64
	series   map[string]int64
	authors  map[AuthorName]int64
	genres   map[string]int64
	keywords map[string]int64

	stats ImportStats
}

type importStmts struct {
	folder, series, author, genre, keyword, book, ba, bg, bk *sql.Stmt
}

const importBatchSize = 25_000

// NewImport starts an import session. The database must be empty —
// incremental collection updates will come as a separate operation.
func (s *Store) NewImport(ctx context.Context) (*ImportSession, error) {
	n, err := s.BookCount(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, fmt.Errorf("library is not empty (%d books); use --replace to reimport", n)
	}

	// Indexes only get in the way during bulk loading.
	for _, idx := range schemaIndexes {
		name := indexName(idx)
		if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return nil, err
		}
	}

	im := &ImportSession{
		s:        s,
		ctx:      ctx,
		start:    time.Now(),
		folders:  make(map[string]int64, 1024),
		series:   make(map[string]int64, 1<<16),
		authors:  make(map[AuthorName]int64, 1<<17),
		genres:   make(map[string]int64, 256),
		keywords: make(map[string]int64, 1<<14),
	}
	if err := im.begin(); err != nil {
		return nil, err
	}
	return im, nil
}

func (im *ImportSession) begin() error {
	tx, err := im.s.db.BeginTx(im.ctx, nil)
	if err != nil {
		return err
	}
	im.tx = tx

	prep := func(q string) *sql.Stmt {
		if err != nil {
			return nil
		}
		var st *sql.Stmt
		st, err = tx.PrepareContext(im.ctx, q)
		return st
	}
	im.stmts = importStmts{
		folder:  prep(`INSERT INTO folders (name) VALUES (?)`),
		series:  prep(`INSERT INTO series (title) VALUES (?)`),
		author:  prep(`INSERT INTO authors (last_name, first_name, middle_name) VALUES (?, ?, ?)`),
		genre:   prep(`INSERT INTO genres (code) VALUES (?)`),
		keyword: prep(`INSERT INTO keywords (name) VALUES (?)`),
		book: prep(`INSERT INTO books (lib_id, title, series_id, series_num, folder_id, file, ext, size, lang, year, added, lib_rate, deleted)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		ba: prep(`INSERT OR IGNORE INTO book_authors (book_id, author_id) VALUES (?, ?)`),
		bg: prep(`INSERT OR IGNORE INTO book_genres (book_id, genre_id) VALUES (?, ?)`),
		bk: prep(`INSERT OR IGNORE INTO book_keywords (book_id, keyword_id) VALUES (?, ?)`),
	}
	return err
}

func (im *ImportSession) commitBatch() error {
	if err := im.tx.Commit(); err != nil {
		return err
	}
	im.batch = 0
	return im.begin()
}

func lookup(cache map[string]int64, key string, ins *sql.Stmt) (int64, error) {
	if id, ok := cache[key]; ok {
		return id, nil
	}
	res, err := ins.Exec(key)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	cache[key] = id
	return id, nil
}

func (im *ImportSession) Add(b *BookInput) error {
	folderID, err := lookup(im.folders, b.Folder, im.stmts.folder)
	if err != nil {
		return fmt.Errorf("folder %q: %w", b.Folder, err)
	}

	var seriesID any
	if b.Series != "" {
		id, err := lookup(im.series, b.Series, im.stmts.series)
		if err != nil {
			return fmt.Errorf("series %q: %w", b.Series, err)
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

	res, err := im.stmts.book.Exec(b.LibID, b.Title, seriesID, seriesNum, folderID,
		b.File, b.Ext, b.Size, b.Lang, year, b.Added, b.Rate, boolToInt(b.Deleted))
	if err != nil {
		return fmt.Errorf("book %q: %w", b.Title, err)
	}
	bookID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	for _, a := range b.Authors {
		id, ok := im.authors[a]
		if !ok {
			res, err := im.stmts.author.Exec(a.Last, a.First, a.Middle)
			if err != nil {
				return fmt.Errorf("author %v: %w", a, err)
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
			im.authors[a] = id
		}
		if _, err := im.stmts.ba.Exec(bookID, id); err != nil {
			return err
		}
	}
	for _, g := range b.Genres {
		id, err := lookup(im.genres, g, im.stmts.genre)
		if err != nil {
			return fmt.Errorf("genre %q: %w", g, err)
		}
		if _, err := im.stmts.bg.Exec(bookID, id); err != nil {
			return err
		}
	}
	for _, k := range b.Keywords {
		id, err := lookup(im.keywords, k, im.stmts.keyword)
		if err != nil {
			return fmt.Errorf("keyword %q: %w", k, err)
		}
		if _, err := im.stmts.bk.Exec(bookID, id); err != nil {
			return err
		}
	}

	im.stats.Books++
	im.batch++
	if im.batch >= importBatchSize {
		return im.commitBatch()
	}
	return nil
}

// Finish commits the data and builds the indexes and the search index.
func (im *ImportSession) Finish() (ImportStats, error) {
	if err := im.tx.Commit(); err != nil {
		return im.stats, err
	}

	for _, idx := range schemaIndexes {
		if _, err := im.s.db.ExecContext(im.ctx, idx); err != nil {
			return im.stats, fmt.Errorf("index: %w", err)
		}
	}

	if _, err := im.s.db.ExecContext(im.ctx, `DELETE FROM book_search`); err != nil {
		return im.stats, err
	}
	if _, err := im.s.db.ExecContext(im.ctx, `
		INSERT INTO book_search (rowid, title, authors, series)
		SELECT b.id,
		       b.title,
		       coalesce((SELECT group_concat(trim(a.last_name || ' ' || a.first_name || ' ' || a.middle_name), ' ')
		                 FROM book_authors ba JOIN authors a ON a.id = ba.author_id
		                 WHERE ba.book_id = b.id), ''),
		       coalesce(s.title, '')
		FROM books b LEFT JOIN series s ON s.id = b.series_id`); err != nil {
		return im.stats, fmt.Errorf("search index: %w", err)
	}

	if _, err := im.s.db.ExecContext(im.ctx, `DELETE FROM authors_search`); err != nil {
		return im.stats, err
	}
	if _, err := im.s.db.ExecContext(im.ctx, `
		INSERT INTO authors_search (rowid, name)
		SELECT id, trim(last_name || ' ' || first_name || ' ' || middle_name) FROM authors`); err != nil {
		return im.stats, fmt.Errorf("authors search index: %w", err)
	}

	if _, err := im.s.db.ExecContext(im.ctx, `DELETE FROM series_search`); err != nil {
		return im.stats, err
	}
	if _, err := im.s.db.ExecContext(im.ctx, `
		INSERT INTO series_search (rowid, title) SELECT id, title FROM series`); err != nil {
		return im.stats, fmt.Errorf("series search index: %w", err)
	}

	if _, err := im.s.db.ExecContext(im.ctx, `ANALYZE`); err != nil {
		return im.stats, err
	}

	im.stats.Authors = len(im.authors)
	im.stats.Series = len(im.series)
	im.stats.Genres = len(im.genres)
	im.stats.Duration = time.Since(im.start)
	return im.stats, nil
}

// Abort rolls back the uncommitted part of the import.
func (im *ImportSession) Abort() {
	if im.tx != nil {
		im.tx.Rollback()
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// indexName extracts the index name from a CREATE INDEX IF NOT EXISTS <name> ON ... statement.
func indexName(createStmt string) string {
	fields := strings.Fields(createStmt)
	for i, f := range fields {
		if strings.EqualFold(f, "EXISTS") && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}
