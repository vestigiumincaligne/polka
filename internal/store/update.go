package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// UpdateStats summarizes an incremental catalog update.
type UpdateStats struct {
	Added     int // new books (new ids)
	Updated   int // existing books whose metadata changed (same id)
	Moved     int // existing books found in another archive/file (same id)
	Unchanged int
	Skipped   int // records whose archive is not in the library (not imported)
	Orphans   int // authors/series/keywords left without books and removed
	Duration  time.Duration
}

// Skip counts a record the caller decided not to apply.
func (u *UpdateSession) Skip() { u.stats.Skipped++ }

// UpdateSession applies a newer inpx catalog on top of the existing one
// without renumbering books: user ratings, lists, reading progress, kosync
// digests and the EPUB cache are keyed by book id, so a book keeps its id as
// long as it can be matched. A book matches by (lib_id, archive, file); if
// that fails but exactly one catalog book has the same lib_id, it is treated
// as the same book moved to another archive. Everything else is added.
// Books missing from the new catalog are left as they are (Flibusta marks
// removals with the DEL flag, which is applied as usual).
type UpdateSession struct {
	s     *Store
	ctx   context.Context
	tx    *sql.Tx
	stmts updateStmts
	start time.Time

	folders  map[string]int64
	series   map[string]int64
	authors  map[AuthorName]int64
	genres   map[string]int64
	keywords map[string]int64

	existing map[string]existingBook // by bookKey
	byLibID  map[string][]int64      // catalog (non-uploaded) books by lib_id
	used     map[int64]bool          // ids already matched in this run

	stats UpdateStats
}

type existingBook struct {
	id  int64
	sig [32]byte
}

type updateStmts struct {
	folder, series, author, genre, keyword *sql.Stmt
	insert, update                         *sql.Stmt
	delBA, delBG, delBK                    *sql.Stmt
	ba, bg, bk                             *sql.Stmt
}

func bookKey(libID, folder, file, ext string) string {
	return libID + "\x00" + folder + "\x00" + file + "\x00" + strings.ToLower(ext)
}

// bookSig fingerprints everything an import writes for a book, so unchanged
// books can be skipped. Lists are sorted: their storage order is not stable.
func bookSig(title, series string, seriesNum int, folder, file, ext string, size int64,
	lang string, year int, added string, rate float64, deleted bool,
	authors, genres, keywords []string) [32]byte {
	// Sorted and de-duplicated: storage order is not stable, and the link
	// tables keep a repeated author/genre/keyword only once.
	authors, genres, keywords = sortedSet(authors), sortedSet(genres), sortedSet(keywords)
	parts := []string{
		title, series, strconv.Itoa(seriesNum), folder, file, strings.ToLower(ext),
		strconv.FormatInt(size, 10), lang, strconv.Itoa(year), added,
		strconv.FormatFloat(rate, 'g', -1, 64), strconv.FormatBool(deleted),
		strings.Join(authors, "\x1e"), strings.Join(genres, "\x1e"), strings.Join(keywords, "\x1e"),
	}
	return sha256.Sum256([]byte(strings.Join(parts, "\x1d")))
}

func sortedSet(list []string) []string {
	sort.Strings(list)
	out := list[:0]
	for i, v := range list {
		if i == 0 || v != list[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func inputSig(b *BookInput) [32]byte {
	authors := make([]string, 0, len(b.Authors))
	for _, a := range b.Authors {
		authors = append(authors, a.Last+"\x1f"+a.First+"\x1f"+a.Middle)
	}
	return bookSig(b.Title, b.Series, max(b.SeriesNum, 0), b.Folder, b.File, b.Ext, b.Size,
		b.Lang, max(b.Year, 0), b.Added, b.Rate, b.Deleted,
		authors, append([]string(nil), b.Genres...), append([]string(nil), b.Keywords...))
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x1e")
}

// NewUpdate starts an incremental update. It loads the current catalog
// (keys, fingerprints and dictionaries) into memory.
func (s *Store) NewUpdate(ctx context.Context) (*UpdateSession, error) {
	u := &UpdateSession{
		s:        s,
		ctx:      ctx,
		start:    time.Now(),
		folders:  map[string]int64{},
		series:   map[string]int64{},
		authors:  map[AuthorName]int64{},
		genres:   map[string]int64{},
		keywords: map[string]int64{},
		existing: map[string]existingBook{},
		byLibID:  map[string][]int64{},
		used:     map[int64]bool{},
	}
	if err := u.loadDicts(); err != nil {
		return nil, err
	}
	if err := u.loadBooks(); err != nil {
		return nil, err
	}
	if err := u.begin(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *UpdateSession) loadDicts() error {
	load := func(q string, fn func(*sql.Rows) error) error {
		rows, err := u.s.db.QueryContext(u.ctx, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	strMap := func(m map[string]int64) func(*sql.Rows) error {
		return func(r *sql.Rows) error {
			var id int64
			var k string
			if err := r.Scan(&id, &k); err != nil {
				return err
			}
			m[k] = id
			return nil
		}
	}
	if err := load(`SELECT id, name FROM folders`, strMap(u.folders)); err != nil {
		return err
	}
	if err := load(`SELECT id, title FROM series`, strMap(u.series)); err != nil {
		return err
	}
	if err := load(`SELECT id, code FROM genres`, strMap(u.genres)); err != nil {
		return err
	}
	if err := load(`SELECT id, name FROM keywords`, strMap(u.keywords)); err != nil {
		return err
	}
	return load(`SELECT id, last_name, first_name, middle_name FROM authors`, func(r *sql.Rows) error {
		var a AuthorName
		var id int64
		if err := r.Scan(&id, &a.Last, &a.First, &a.Middle); err != nil {
			return err
		}
		u.authors[a] = id
		return nil
	})
}

func (u *UpdateSession) loadBooks() error {
	rows, err := u.s.db.QueryContext(u.ctx, `
		SELECT b.id, b.lib_id, f.name, b.file, b.ext, b.title, coalesce(s.title, ''),
		       coalesce(b.series_num, 0), b.size, b.lang, coalesce(b.year, 0), b.added,
		       b.lib_rate, b.deleted, b.file_hash,
		       coalesce((SELECT group_concat(a.last_name || char(31) || a.first_name || char(31) || a.middle_name, char(30))
		                 FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE ba.book_id = b.id), ''),
		       coalesce((SELECT group_concat(g.code, char(30))
		                 FROM book_genres bg JOIN genres g ON g.id = bg.genre_id WHERE bg.book_id = b.id), ''),
		       coalesce((SELECT group_concat(k.name, char(30))
		                 FROM book_keywords bk JOIN keywords k ON k.id = bk.keyword_id WHERE bk.book_id = b.id), '')
		FROM books b JOIN folders f ON f.id = b.folder_id
		LEFT JOIN series s ON s.id = b.series_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, size                                                 int64
			libID, folder, file, ext, title, series, lang, added, fh string
			seriesNum, year                                          int
			rate                                                     float64
			deleted                                                  bool
			authors, genres, keywords                                string
		)
		if err := rows.Scan(&id, &libID, &folder, &file, &ext, &title, &series, &seriesNum,
			&size, &lang, &year, &added, &rate, &deleted, &fh, &authors, &genres, &keywords); err != nil {
			return err
		}
		sig := bookSig(title, series, seriesNum, folder, file, ext, size, lang, year, added, rate, deleted,
			splitList(authors), splitList(genres), splitList(keywords))
		key := bookKey(libID, folder, file, ext)
		if _, dup := u.existing[key]; !dup {
			u.existing[key] = existingBook{id: id, sig: sig}
		}
		if fh == "" && libID != "" { // uploaded books are never matched by lib_id
			u.byLibID[libID] = append(u.byLibID[libID], id)
		}
	}
	return rows.Err()
}

func (u *UpdateSession) begin() error {
	tx, err := u.s.db.BeginTx(u.ctx, nil)
	if err != nil {
		return err
	}
	u.tx = tx
	prep := func(q string) *sql.Stmt {
		if err != nil {
			return nil
		}
		var st *sql.Stmt
		st, err = tx.PrepareContext(u.ctx, q)
		return st
	}
	u.stmts = updateStmts{
		folder:  prep(`INSERT INTO folders (name) VALUES (?)`),
		series:  prep(`INSERT INTO series (title) VALUES (?)`),
		author:  prep(`INSERT INTO authors (last_name, first_name, middle_name) VALUES (?, ?, ?)`),
		genre:   prep(`INSERT INTO genres (code) VALUES (?)`),
		keyword: prep(`INSERT INTO keywords (name) VALUES (?)`),
		insert: prep(`INSERT INTO books (lib_id, title, series_id, series_num, folder_id, file, ext, size, lang, year, added, lib_rate, deleted)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		update: prep(`UPDATE books SET lib_id = ?, title = ?, series_id = ?, series_num = ?, folder_id = ?, file = ?, ext = ?,
			size = ?, lang = ?, year = ?, added = ?, lib_rate = ?, deleted = ? WHERE id = ?`),
		delBA: prep(`DELETE FROM book_authors WHERE book_id = ?`),
		delBG: prep(`DELETE FROM book_genres WHERE book_id = ?`),
		delBK: prep(`DELETE FROM book_keywords WHERE book_id = ?`),
		ba:    prep(`INSERT OR IGNORE INTO book_authors (book_id, author_id) VALUES (?, ?)`),
		bg:    prep(`INSERT OR IGNORE INTO book_genres (book_id, genre_id) VALUES (?, ?)`),
		bk:    prep(`INSERT OR IGNORE INTO book_keywords (book_id, keyword_id) VALUES (?, ?)`),
	}
	return err
}

// match returns the id of the existing book this record updates (0 = new)
// and whether it was found by lib_id only.
func (u *UpdateSession) match(b *BookInput) (id int64, moved bool, sig [32]byte, same bool) {
	sig = inputSig(b)
	if e, ok := u.existing[bookKey(b.LibID, b.Folder, b.File, b.Ext)]; ok && !u.used[e.id] {
		return e.id, false, sig, e.sig == sig
	}
	if b.LibID != "" {
		if ids := u.byLibID[b.LibID]; len(ids) == 1 && !u.used[ids[0]] {
			return ids[0], true, sig, false
		}
	}
	return 0, false, sig, false
}

// Add applies one record of the new catalog.
func (u *UpdateSession) Add(b *BookInput) error {
	id, moved, _, same := u.match(b)
	if id != 0 {
		u.used[id] = true
	}
	if id != 0 && same {
		u.stats.Unchanged++
		return nil
	}

	folderID, err := lookup(u.folders, b.Folder, u.stmts.folder)
	if err != nil {
		return fmt.Errorf("folder %q: %w", b.Folder, err)
	}
	var seriesID, seriesNum, year any
	if b.Series != "" {
		sid, err := lookup(u.series, b.Series, u.stmts.series)
		if err != nil {
			return fmt.Errorf("series %q: %w", b.Series, err)
		}
		seriesID = sid
	}
	if b.SeriesNum > 0 {
		seriesNum = b.SeriesNum
	}
	if b.Year > 0 {
		year = b.Year
	}

	if id == 0 {
		res, err := u.stmts.insert.Exec(b.LibID, b.Title, seriesID, seriesNum, folderID,
			b.File, b.Ext, b.Size, b.Lang, year, b.Added, b.Rate, boolToInt(b.Deleted))
		if err != nil {
			return fmt.Errorf("book %q: %w", b.Title, err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		u.used[id] = true
		u.stats.Added++
	} else {
		if _, err := u.stmts.update.Exec(b.LibID, b.Title, seriesID, seriesNum, folderID,
			b.File, b.Ext, b.Size, b.Lang, year, b.Added, b.Rate, boolToInt(b.Deleted), id); err != nil {
			return fmt.Errorf("book %d %q: %w", id, b.Title, err)
		}
		for _, st := range []*sql.Stmt{u.stmts.delBA, u.stmts.delBG, u.stmts.delBK} {
			if _, err := st.Exec(id); err != nil {
				return err
			}
		}
		if moved {
			u.stats.Moved++
		} else {
			u.stats.Updated++
		}
	}

	for _, a := range b.Authors {
		aid, ok := u.authors[a]
		if !ok {
			res, err := u.stmts.author.Exec(a.Last, a.First, a.Middle)
			if err != nil {
				return fmt.Errorf("author %v: %w", a, err)
			}
			if aid, err = res.LastInsertId(); err != nil {
				return err
			}
			u.authors[a] = aid
		}
		if _, err := u.stmts.ba.Exec(id, aid); err != nil {
			return err
		}
	}
	for _, g := range b.Genres {
		gid, err := lookup(u.genres, g, u.stmts.genre)
		if err != nil {
			return fmt.Errorf("genre %q: %w", g, err)
		}
		if _, err := u.stmts.bg.Exec(id, gid); err != nil {
			return err
		}
	}
	for _, k := range b.Keywords {
		kid, err := lookup(u.keywords, k, u.stmts.keyword)
		if err != nil {
			return fmt.Errorf("keyword %q: %w", k, err)
		}
		if _, err := u.stmts.bk.Exec(id, kid); err != nil {
			return err
		}
	}

	// One transaction for the whole update: a failure leaves the catalog as it was.
	return nil
}

// Finish commits and rebuilds the search indexes.
func (u *UpdateSession) Finish() (UpdateStats, error) {
	if err := u.dropOrphans(); err != nil {
		return u.stats, err
	}
	if err := u.tx.Commit(); err != nil {
		return u.stats, err
	}
	u.tx = nil
	if err := u.s.rebuildSearch(u.ctx); err != nil {
		return u.stats, err
	}
	u.stats.Duration = time.Since(u.start)
	return u.stats, nil
}

// dropOrphans removes authors, series and keywords left without books:
// renamed ones keep their old rows otherwise, which a fresh import never has.
func (u *UpdateSession) dropOrphans() error {
	for _, q := range []string{
		`DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors)`,
		`DELETE FROM series WHERE id NOT IN (SELECT series_id FROM books WHERE series_id IS NOT NULL)`,
		`DELETE FROM keywords WHERE id NOT IN (SELECT keyword_id FROM book_keywords)`,
	} {
		res, err := u.tx.ExecContext(u.ctx, q)
		if err != nil {
			return fmt.Errorf("orphans: %w", err)
		}
		n, _ := res.RowsAffected()
		u.stats.Orphans += int(n)
	}
	return nil
}

// Rollback discards everything this session did and returns what it would
// have changed (dry run).
func (u *UpdateSession) Rollback() (UpdateStats, error) {
	if err := u.dropOrphans(); err != nil {
		return u.stats, err
	}
	err := u.tx.Rollback()
	u.tx = nil
	u.stats.Duration = time.Since(u.start)
	return u.stats, err
}

// Abort rolls back the uncommitted part of the update.
func (u *UpdateSession) Abort() {
	if u.tx != nil {
		u.tx.Rollback()
	}
}
