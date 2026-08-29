// Package collections implements themed book collections ("100 books by Forbes",
// award winners): an external "author + title" list matched against the
// books in the library. It is stored in a separate collections.db database so
// that an inpx re-import does not wipe the lists; the matching (book_id) is
// recomputed after every import.
package collections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/vestigiumincaligne/polka/internal/sqlitedrv"
	"github.com/vestigiumincaligne/polka/internal/store"
)

const schema = `
CREATE TABLE IF NOT EXISTS collections (
	id          INTEGER PRIMARY KEY,
	slug        TEXT NOT NULL UNIQUE,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	source      TEXT NOT NULL DEFAULT '',
	url         TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL DEFAULT (datetime('now')),
	matched_at  TEXT NOT NULL DEFAULT '',
	bundled     INTEGER NOT NULL DEFAULT 0,
	origin      TEXT NOT NULL DEFAULT ''
);

-- Collections from bundled files and external sources that the administrator
-- deleted: they are not restored on the next load/sync.
CREATE TABLE IF NOT EXISTS hidden_bundled (
	slug TEXT PRIMARY KEY
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS collection_items (
	id            INTEGER PRIMARY KEY,
	collection_id INTEGER NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
	position      INTEGER NOT NULL,
	title         TEXT NOT NULL,
	author        TEXT NOT NULL DEFAULT '',
	isbn          TEXT NOT NULL DEFAULT '',
	year          INTEGER NOT NULL DEFAULT 0,
	note          TEXT NOT NULL DEFAULT '',
	book_id       INTEGER NOT NULL DEFAULT 0,
	match_kind    TEXT NOT NULL DEFAULT '',
	UNIQUE (collection_id, position)
);
CREATE INDEX IF NOT EXISTS idx_items_book ON collection_items (book_id) WHERE book_id != 0;
`

// OriginBundled marks a collection from the files bundled with Polka.
const OriginBundled = "bundled"

// Bundled reports whether the collection ships with Polka.
func (c *Collection) Bundled() bool { return c.Origin == OriginBundled }

var (
	ErrNotFound = errors.New("collection not found")
	ErrInvalid  = errors.New("invalid collection")
)

// File is the collection file format (JSON):
//
//	{
//	  "slug": "forbes-100",
//	  "title": "100 книг по версии Forbes",
//	  "description": "…", "source": "Forbes", "url": "https://…",
//	  "items": [
//	    {"title": "1984", "author": "Джордж Оруэлл", "isbn": "9785170800902", "year": 1949, "note": "…"},
//	    "Оруэлл — 1984"
//	  ]
//	}
//
// An item may also be a string "Author — Title" / "Author. Title".
type File struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	URL         string     `json:"url"`
	Items       []FileItem `json:"items"`
}

// FileItem is an entry of a collection file.
type FileItem struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	ISBN   string `json:"isbn"`
	Year   int    `json:"year"`
	Note   string `json:"note"`
}

// UnmarshalJSON accepts both an object and an "Author — Title" string.
func (it *FileItem) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		it.Author, it.Title = splitAuthorTitle(s)
		return nil
	}
	type plain FileItem
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*it = FileItem(p)
	return nil
}

// splitAuthorTitle splits "Author — Title", "Author – Title", "Author. Title",
// "Author: Title". Without a separator the whole string is the title.
func splitAuthorTitle(s string) (author, title string) {
	s = strings.TrimSpace(s)
	for _, sep := range []string{" — ", " – ", " -- ", ": ", ". "} {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):])
		}
	}
	return "", s
}

// Parse reads and validates a collection file.
func Parse(r io.Reader) (*File, error) {
	var f File
	dec := json.NewDecoder(r)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	f.Title = strings.TrimSpace(f.Title)
	if f.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	f.Slug = Slugify(f.Slug)
	if f.Slug == "" {
		f.Slug = Slugify(f.Title)
	}
	if f.Slug == "" {
		return nil, fmt.Errorf("%w: slug is required", ErrInvalid)
	}
	items := f.Items[:0]
	for _, it := range f.Items {
		it.Title = strings.TrimSpace(it.Title)
		it.Author = strings.TrimSpace(it.Author)
		if unknownAuthor(it.Author) {
			it.Author = ""
		}
		if it.Title == "" {
			continue
		}
		items = append(items, it)
	}
	f.Items = items
	if len(f.Items) == 0 {
		return nil, fmt.Errorf("%w: no items", ErrInvalid)
	}
	return &f, nil
}

// unknownAuthor reports placeholder authors ("Неизвестный автор", "Аноним", "народное"):
// searching by such an author is pointless, so we match by title only.
func unknownAuthor(a string) bool {
	l := strings.ToLower(a)
	for _, w := range []string{"неизвестн", "аноним", "народн", "unknown", "anonymous", "эпосы", "фольклор"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

var slugRe = regexp.MustCompile(`[^a-z0-9а-яё]+`)

// Slugify derives a collection identifier from an arbitrary string: lowercase
// Latin and Cyrillic letters, everything else becomes hyphens.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
		s = strings.Trim(s, "-")
	}
	return s
}

// --- Service ---

type Service struct {
	db *sql.DB
}

func Open(path string) (*Service, error) {
	db, err := sqlitedrv.Open(path, sqlitedrv.Options{
		WAL: true, BusyTimeout: 10000, ForeignKeys: true,
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("collections schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("collections migrate: %w", err)
	}
	return &Service{db: db}, nil
}

func (s *Service) Close() error { return s.db.Close() }

// migrate upgrades databases created before the newer columns existed.
func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(collections)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasBundled, hasOrigin := false, false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var def any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			return err
		}
		if name == "bundled" {
			hasBundled = true
		}
		if name == "origin" {
			hasOrigin = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasBundled {
		if _, err := db.Exec(`ALTER TABLE collections ADD COLUMN bundled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !hasOrigin {
		if _, err := db.Exec(`ALTER TABLE collections ADD COLUMN origin TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE collections SET origin = 'bundled' WHERE bundled = 1`); err != nil {
			return err
		}
	}
	return nil
}

// Collection is a collection with counters.
type Collection struct {
	ID          int64
	Slug        string
	Title       string
	Description string
	Source      string
	URL         string
	CreatedAt   string
	MatchedAt   string
	Origin      string // '' — uploaded manually, OriginBundled — from Polka's files, otherwise the external source id
	Total       int    // entries in the list
	Matched     int    // found in the library
}

// Item is a collection entry; BookID != 0 means it was found in the library.
type Item struct {
	ID        int64
	Position  int
	Title     string
	Author    string
	ISBN      string
	Year      int
	Note      string
	BookID    int64
	MatchKind string
}

const collectionCols = `
	c.id, c.slug, c.title, c.description, c.source, c.url, c.created_at, c.matched_at, c.origin,
	(SELECT count(*) FROM collection_items i WHERE i.collection_id = c.id),
	(SELECT count(*) FROM collection_items i WHERE i.collection_id = c.id AND i.book_id != 0)`

func scanCollection(row interface{ Scan(...any) error }) (*Collection, error) {
	var c Collection
	err := row.Scan(&c.ID, &c.Slug, &c.Title, &c.Description, &c.Source, &c.URL,
		&c.CreatedAt, &c.MatchedAt, &c.Origin, &c.Total, &c.Matched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// Import creates a collection or fully replaces the one with the same slug.
// A manually uploaded collection stops being bundled/external.
func (s *Service) Import(ctx context.Context, f *File) (*Collection, error) {
	return s.ImportFrom(ctx, f, "")
}

// ImportFrom is Import with an origin mark (see Collection.Origin).
func (s *Service) ImportFrom(ctx context.Context, f *File, origin string) (*Collection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM collections WHERE slug = ?`, f.Slug).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `
			INSERT INTO collections (slug, title, description, source, url, origin)
			VALUES (?, ?, ?, ?, ?, ?)`, f.Slug, f.Title, f.Description, f.Source, f.URL, origin)
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return nil, err
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE collections SET title = ?, description = ?, source = ?, url = ?, matched_at = '', origin = ?
			WHERE id = ?`, f.Title, f.Description, f.Source, f.URL, origin, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM collection_items WHERE collection_id = ?`, id); err != nil {
			return nil, err
		}
	}
	for i, it := range f.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO collection_items (collection_id, position, title, author, isbn, year, note)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, i+1, it.Title, it.Author, store.NormalizeISBN(it.ISBN), it.Year, it.Note); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, f.Slug)
}

// List returns all collections (ordered by title).
func (s *Service) List(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT`+collectionCols+` FROM collections c ORDER BY c.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, slug string) (*Collection, error) {
	return scanCollection(s.db.QueryRowContext(ctx,
		`SELECT`+collectionCols+` FROM collections c WHERE c.slug = ?`, slug))
}

// Items returns the collection's entries in list order.
func (s *Service) Items(ctx context.Context, collectionID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, position, title, author, isbn, year, note, book_id, match_kind
		FROM collection_items WHERE collection_id = ? ORDER BY position`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Position, &it.Title, &it.Author, &it.ISBN,
			&it.Year, &it.Note, &it.BookID, &it.MatchKind); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// BookIDs returns the ids of the collection's matched books in list order,
// paginated (for the shelf). Returns limit+1 rows when there is a next page,
// like ListBookIDs for lists.
func (s *Service) BookIDs(ctx context.Context, collectionID int64, limit, offset int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT book_id FROM collection_items
		WHERE collection_id = ? AND book_id != 0
		ORDER BY position LIMIT ? OFFSET ?`, collectionID, limit, offset)
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

// CollectionsOfBook returns the collections that include the book.
func (s *Service) CollectionsOfBook(ctx context.Context, bookID int64) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT`+collectionCols+` FROM collections c
		WHERE c.id IN (SELECT collection_id FROM collection_items WHERE book_id = ?)
		ORDER BY c.title`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Service) Delete(ctx context.Context, slug string) error {
	c, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if c.Origin != "" {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO hidden_bundled (slug) VALUES (?)`, slug); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, c.ID)
	return err
}

// Hidden reports whether the administrator deleted a collection with this slug; do not restore it.
func (s *Service) Hidden(ctx context.Context, slug string) bool {
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM hidden_bundled WHERE slug = ?`, slug).Scan(&n)
	return n > 0
}

// SlugsByOrigin returns the slugs of collections with the given origin (what is
// already loaded from the source, so that articles are not downloaded again).
func (s *Service) SlugsByOrigin(ctx context.Context, origin string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug FROM collections WHERE origin = ?`, origin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out[slug] = true
	}
	return out, rows.Err()
}

// SeedBundled loads the bundled collections (*.json from fsys): new ones are
// added, already loaded bundled ones are updated from the file; those deleted
// by the administrator or uploaded manually under the same slug are left alone.
// Returns the slugs of the added/updated collections.
func (s *Service) SeedBundled(ctx context.Context, fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var seeded []string
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		r, err := fsys.Open(e.Name())
		if err != nil {
			return seeded, err
		}
		f, perr := Parse(r)
		r.Close()
		if perr != nil {
			return seeded, fmt.Errorf("%s: %w", e.Name(), perr)
		}
		if s.Hidden(ctx, f.Slug) {
			continue
		}
		if c, err := s.Get(ctx, f.Slug); err == nil && !c.Bundled() {
			continue // manual collection under the same slug; do not overwrite
		}
		if _, err := s.ImportFrom(ctx, f, OriginBundled); err != nil {
			return seeded, fmt.Errorf("%s: %w", e.Name(), err)
		}
		seeded = append(seeded, f.Slug)
	}
	return seeded, nil
}

// Count returns the number of collections (for the UI config).
func (s *Service) Count(ctx context.Context) int {
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM collections`).Scan(&n)
	return n
}

// --- Matching against the library ---

// Matcher is what we search books with (store.Store).
type Matcher interface {
	MatchBookExt(ctx context.Context, title, author, isbn string) (*store.Book, string, error)
}

// MatchStats is the result of matching one collection.
type MatchStats struct {
	Slug    string
	Total   int
	Matched int
}

// Match recomputes the matching of a collection against the library.
func (s *Service) Match(ctx context.Context, m Matcher, slug string) (*MatchStats, error) {
	c, err := s.Get(ctx, slug)
	if err != nil {
		return nil, err
	}
	items, err := s.Items(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	st := &MatchStats{Slug: slug, Total: len(items)}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		var bookID int64
		var kind string
		if b, k, err := m.MatchBookExt(ctx, it.Title, it.Author, it.ISBN); err == nil {
			bookID, kind = b.ID, k
			st.Matched++
		}
		if bookID != it.BookID || kind != it.MatchKind {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE collection_items SET book_id = ?, match_kind = ? WHERE id = ?`,
				bookID, kind, it.ID); err != nil {
				return st, err
			}
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE collections SET matched_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), c.ID)
	return st, err
}

// MatchAll recomputes all collections.
func (s *Service) MatchAll(ctx context.Context, m Matcher) ([]MatchStats, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []MatchStats
	for _, c := range list {
		st, err := s.Match(ctx, m, c.Slug)
		if err != nil {
			return out, err
		}
		out = append(out, *st)
	}
	return out, nil
}
