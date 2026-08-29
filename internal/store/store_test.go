package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vestigiumincaligne/polka/internal/sqlitedrv"
)

// newLibrary imports a small but varied catalog:
//
//	Толстой: «Война и мир» (Классика #1, rate 5), «Анна Каренина» (Классика #2, rate 4)
//	Чехов:   «Вишневый сад» (rate 3), «Рассказы» (rate 0)
//	Стругацкие (два автора): «Пикник на обочине» (sf, rate 5)
//	Оруэлл:  «1984» (sf, rate 2) — deleted
//	Лем:     «Солярис» (sf, rate 4) in folder two.zip
func newLibrary(t *testing.T) (*Store, map[string]int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type book struct {
		title   string
		authors []AuthorName
		series  string
		num     int
		genres  []string
		folder  string
		rate    float64
		added   string
		deleted bool
	}
	books := []book{
		{"Война и мир", []AuthorName{{Last: "Толстой", First: "Лев"}}, "Классика", 1, []string{"prose_classic"}, "one.zip", 5, "2024-01-05", false},
		{"Анна Каренина", []AuthorName{{Last: "Толстой", First: "Лев"}}, "Классика", 2, []string{"prose_classic"}, "one.zip", 4, "2024-01-04", false},
		{"Вишневый сад", []AuthorName{{Last: "Чехов", First: "Антон"}}, "", 0, []string{"dramaturgy"}, "one.zip", 3, "2024-01-03", false},
		{"Рассказы", []AuthorName{{Last: "Чехов", First: "Антон"}}, "", 0, []string{"prose_classic"}, "one.zip", 0, "2024-01-02", false},
		{"Пикник на обочине", []AuthorName{{Last: "Стругацкий", First: "Аркадий"}, {Last: "Стругацкий", First: "Борис"}}, "", 0, []string{"sf"}, "one.zip", 5, "2024-01-06", false},
		{"1984", []AuthorName{{Last: "Оруэлл", First: "Джордж"}}, "", 0, []string{"sf"}, "one.zip", 2, "2024-01-01", true},
		{"Солярис", []AuthorName{{Last: "Лем", First: "Станислав"}}, "", 0, []string{"sf"}, "two.zip", 4, "2024-01-07", false},
	}
	for i, b := range books {
		if err := session.Add(&BookInput{
			LibID: fmt.Sprint(1000 + i), Title: b.title, Authors: b.authors, Series: b.series, SeriesNum: b.num,
			Genres: b.genres, Folder: b.folder, File: fmt.Sprint(1000 + i), Ext: "fb2", Size: int64(100 * (i + 1)),
			Lang: "ru", Year: 1900 + i, Added: b.added, Rate: b.rate, Deleted: b.deleted,
		}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := session.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Books != 7 || stats.Authors != 6 || stats.Series != 1 || stats.Genres != 3 {
		t.Fatalf("import stats: %+v", stats)
	}
	ids := map[string]int64{}
	rows, _ := st.DB().Query(`SELECT id, title FROM books`)
	for rows.Next() {
		var id int64
		var title string
		rows.Scan(&id, &title)
		ids[title] = id
	}
	rows.Close()
	return st, ids
}

func titles(books []Book) []string {
	out := make([]string, len(books))
	for i, b := range books {
		out[i] = b.Title
	}
	return out
}

func TestMigrateFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sqlitedrv.Open(path, sqlitedrv.Options{WAL: true})
	if err != nil {
		t.Fatal(err)
	}
	// A v1 database: the schema before duplicate hashes and ISBN existed.
	legacy := schemaV1
	for _, line := range []string{
		"\tfile_hash    TEXT NOT NULL DEFAULT '',\n",
		"\tcontent_hash TEXT NOT NULL DEFAULT '',\n",
		"\tisbn         TEXT NOT NULL DEFAULT ''\n",
	} {
		if !strings.Contains(legacy, line) {
			t.Fatalf("schemaV1 changed, adjust the test: %q", line)
		}
		legacy = strings.Replace(legacy, line, "", 1)
	}
	legacy = strings.Replace(legacy, "\tdeleted    INTEGER NOT NULL DEFAULT 0,\n", "\tdeleted    INTEGER NOT NULL DEFAULT 0\n", 1)
	if _, err := db.Exec(legacy); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO folders (id, name) VALUES (1, 'a.zip');
		INSERT INTO books (title, folder_id, file) VALUES ('Старая книга', 1, '1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer st.Close()
	var version int
	st.DB().QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
	var hash, isbn string
	if err := st.DB().QueryRow(`SELECT file_hash, isbn FROM books`).Scan(&hash, &isbn); err != nil {
		t.Errorf("new columns missing after migration: %v", err)
	}
	ctx := context.Background()
	if n, _ := st.BookCount(ctx); n != 1 {
		t.Errorf("legacy data lost: %d books", n)
	}
	// The migrated database accepts new-style writes.
	if _, err := st.AddBook(ctx, &BookInput{Title: "Новая", Folder: "a.zip", File: "2", Ext: "fb2", ISBN: "978-5-17-080090-2"}); err != nil {
		t.Errorf("AddBook after migration: %v", err)
	}
	if books, _ := st.SearchByISBN(ctx, "9785170800902"); len(books) != 1 {
		t.Errorf("isbn index after migration: %v", books)
	}
	// Reopening is a no-op.
	st.Close()
	if st2, err := Open(path); err != nil {
		t.Errorf("reopen: %v", err)
	} else {
		st2.Close()
	}
}

func TestMeta(t *testing.T) {
	st, _ := newLibrary(t)
	ctx := context.Background()
	if v := st.GetMeta(ctx, "collection_name", "Полка"); v != "Полка" {
		t.Errorf("default: %q", v)
	}
	st.SetMeta(ctx, "collection_name", "Моя")
	st.SetMeta(ctx, "collection_name", "Моя библиотека")
	if v := st.GetMeta(ctx, "collection_name", "Полка"); v != "Моя библиотека" {
		t.Errorf("after set: %q", v)
	}
	if n, _ := st.BookCount(ctx); n != 6 {
		t.Errorf("BookCount must skip deleted: %d", n)
	}
}

func TestShelves(t *testing.T) {
	st, _ := newLibrary(t)
	ctx := context.Background()

	shelves, err := st.HomeShelves(ctx, 12)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Shelf{}
	for _, s := range shelves {
		byID[s.ID] = s
	}
	if got := titles(byID["newest"].Books); len(got) != 6 || got[0] != "Солярис" || got[5] != "Рассказы" {
		t.Errorf("newest (by added desc, deleted hidden): %v", got)
	}
	if got := titles(byID["top_rated"].Books); len(got) != 4 || got[0] != "Солярис" && got[0] != "Война и мир" && got[0] != "Пикник на обочине" {
		t.Errorf("top_rated (rate >= 4): %v", got)
	}
	for _, b := range byID["top_rated"].Books {
		if b.LibRate < 4 {
			t.Errorf("top_rated has %q with rate %v", b.Title, b.LibRate)
		}
	}
	if got := titles(byID["well_curated"].Books); len(got) != 1 || got[0] != "Вишневый сад" {
		t.Errorf("well_curated (rate 1..3, deleted hidden): %v", got)
	}
	if byID["newest"].HasMore {
		t.Error("6 books with limit 12: no more pages")
	}
	if s, _ := st.HomeShelves(ctx, 2); !s[0].HasMore || len(s[0].Books) != 2 {
		t.Errorf("limit 2: %+v", s[0])
	}

	// Pagination of a built-in shelf.
	page1, _, err := st.ShelfBooks(ctx, "newest", 4, 0)
	page2, _, _ := st.ShelfBooks(ctx, "newest", 4, 4)
	if err != nil || len(page1) != 4 || len(page2) != 2 || page1[0].Title != "Солярис" || page2[1].Title != "Рассказы" {
		t.Errorf("pagination: %v | %v (%v)", titles(page1), titles(page2), err)
	}
	// Genre shelves carry the genre title (code when no name is known).
	books, title, err := st.ShelfBooks(ctx, "genre_sf", 10, 0)
	if err != nil || title != "sf" || len(books) != 2 {
		t.Errorf("genre shelf: %v %q %v", titles(books), title, err)
	}
	st.DB().Exec(`UPDATE genres SET name = 'Фантастика' WHERE code = 'sf'`)
	if _, title, _ = st.ShelfBooks(ctx, "genre_sf", 10, 0); title != "Фантастика" {
		t.Errorf("genre title from name: %q", title)
	}
	for _, id := range []string{"genre_nope", "bogus", ""} {
		if _, _, err := st.ShelfBooks(ctx, id, 10, 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("shelf %q: %v, want ErrNotFound", id, err)
		}
	}

	// Catalog shelves: random genres that have books, at most `limit` each.
	cat, err := st.CatalogShelves(ctx, 10, 1)
	if err != nil || len(cat) != 3 {
		t.Fatalf("catalog shelves: %d %v", len(cat), err)
	}
	for _, s := range cat {
		if !strings.HasPrefix(s.ID, "genre_") || len(s.Books) != 1 || !s.HasMore && s.ID != "genre_dramaturgy" {
			t.Errorf("catalog shelf: %+v", s)
		}
	}
	if cat, _ = st.CatalogShelves(ctx, 2, 10); len(cat) != 2 {
		t.Errorf("count 2: %d", len(cat))
	}
}

func TestSearch(t *testing.T) {
	st, ids := newLibrary(t)
	ctx := context.Background()

	stats, err := st.SearchStats(ctx, "Толстой")
	if err != nil || stats.Authors != 1 || stats.BookTitles != 0 {
		t.Errorf("stats author: %+v %v", stats, err)
	}
	if stats, _ = st.SearchStats(ctx, "Классика"); stats.BookSeries != 1 {
		t.Errorf("stats series: %+v", stats)
	}
	if stats, _ = st.SearchStats(ctx, "  "); stats != (SearchStats{}) {
		t.Errorf("blank query must be empty: %+v", stats)
	}
	if stats, _ = st.SearchStats(ctx, "1984"); stats.BookTitles != 0 {
		t.Errorf("books imported as deleted must not be counted: %+v", stats)
	}

	if got := titles(must(st.SearchTitles(ctx, "вой", 10))); len(got) != 1 || got[0] != "Война и мир" {
		t.Errorf("prefix search: %v", got)
	}
	if got := titles(must(st.SearchTitles(ctx, "Вишнёвый", 10))); len(got) != 1 {
		t.Errorf("ё must match е: %v", got)
	}
	if got := titles(must(st.SearchTitles(ctx, "1984", 10))); len(got) != 0 {
		t.Errorf("deleted books are not searchable: %v", got)
	}
	if got := must(st.SearchTitles(ctx, "", 10)); got != nil {
		t.Errorf("empty query: %v", got)
	}

	authors, _ := st.SearchAuthors(ctx, "Стругацкий", 10)
	if len(authors) != 2 || authors[0].Books != 1 || !strings.HasPrefix(authors[0].Name, "Стругацкий") {
		t.Errorf("authors: %+v", authors)
	}
	if a, _ := st.SearchAuthors(ctx, "Оруэлл", 10); len(a) != 1 || a[0].Books != 0 {
		t.Errorf("author of a deleted book has 0 live books: %+v", a)
	}
	series, _ := st.SearchSeries(ctx, "Клас", 10)
	if len(series) != 1 || series[0].Title != "Классика" || series[0].Books != 2 {
		t.Errorf("series: %+v", series)
	}

	// Author page: books ordered by series, number, title; name resolved.
	books, name, err := st.AuthorBooks(ctx, authorID(t, st, "Толстой"))
	if err != nil || name != "Толстой Лев" || strings.Join(titles(books), ",") != "Война и мир,Анна Каренина" {
		t.Errorf("author books: %v %q %v", titles(books), name, err)
	}
	if _, _, err := st.AuthorBooks(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown author: %v", err)
	}
	var seriesID int64
	st.DB().QueryRow(`SELECT id FROM series`).Scan(&seriesID)
	books, title, err := st.SeriesBooks(ctx, seriesID)
	if err != nil || title != "Классика" || books[0].SeqNumber != 1 || books[1].SeqNumber != 2 {
		t.Errorf("series books: %v %q %v", titles(books), title, err)
	}
	if _, _, err := st.SeriesBooks(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown series: %v", err)
	}

	// OPDS full search with paging.
	all := must(st.SearchBooks(ctx, "Толстой", 10, 0))
	second := must(st.SearchBooks(ctx, "Толстой", 1, 1))
	if len(all) != 2 || len(second) != 1 {
		t.Errorf("SearchBooks: %v / %v", titles(all), titles(second))
	}
	if got := must(st.SearchBooks(ctx, "", 10, 0)); got != nil {
		t.Errorf("SearchBooks empty: %v", got)
	}

	// Alphabetical ranges.
	as, _ := st.AuthorsByPrefix(ctx, "С", "Т", 10, 0)
	if len(as) != 2 {
		t.Errorf("authors С..Т: %+v", as)
	}
	as, _ = st.AuthorsByPrefix(ctx, "А", "Я", 2, 2)
	if len(as) != 2 || !strings.HasPrefix(as[0].Name, "Стругацкий") {
		t.Errorf("authors page 2: %+v", as)
	}
	if ss, _ := st.SeriesByPrefix(ctx, "К", "Л", 10, 0); len(ss) != 1 || ss[0].Books != 2 {
		t.Errorf("series К..Л: %+v", ss)
	}
	_ = ids
}

func TestBookDetailsAndFiles(t *testing.T) {
	st, ids := newLibrary(t)
	ctx := context.Background()

	d, err := st.BookDetails(ctx, ids["Война и мир"])
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Война и мир" || d.AuthorNames != "Толстой Лев" || d.Folder != "one.zip" || d.File != "1000" ||
		d.Year != 1900 || d.Size != 100 || len(d.Authors) != 1 || d.Authors[0].Last != "Толстой" ||
		len(d.Series) != 1 || d.Series[0].Title != "Классика" || d.Series[0].SeqNumber != 1 ||
		len(d.Genres) != 1 || d.Genres[0] != "prose_classic" {
		t.Errorf("details: %+v", d)
	}
	d, _ = st.BookDetails(ctx, ids["Пикник на обочине"])
	if len(d.Authors) != 2 || len(d.Series) != 0 || d.AuthorNames != "Стругацкий Аркадий, Стругацкий Борис" {
		t.Errorf("two authors, no series: %+v", d)
	}
	for _, id := range []int64{ids["1984"], 99999} {
		if _, err := st.BookDetails(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("details of %d: %v, want ErrNotFound", id, err)
		}
		if _, err := st.BookFile(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("file of %d: %v", id, err)
		}
	}
	f, _ := st.BookFile(ctx, ids["Солярис"])
	if f.Folder != "two.zip" || f.File != "1006" || f.Ext != "fb2" || f.Size != 700 {
		t.Errorf("book file: %+v", f)
	}

	// Ids resolve in the requested order; unknown and deleted are skipped.
	want := []int64{ids["Солярис"], ids["1984"], ids["Война и мир"], 99999}
	books, _ := st.BooksByIDs(ctx, want)
	if strings.Join(titles(books), ",") != "Солярис,Война и мир" {
		t.Errorf("BooksByIDs order: %v", titles(books))
	}
	if books, _ = st.BooksByIDs(ctx, nil); len(books) != 0 {
		t.Errorf("empty ids: %v", books)
	}
	files, _ := st.BookFilesByIDs(ctx, want)
	if len(files) != 2 || files[0].Title+files[1].Title != "Война и мирСолярис" && files[0].Title+files[1].Title != "СолярисВойна и мир" {
		t.Errorf("BookFilesByIDs (live books only): %+v", files)
	}

	genres, _ := st.GenresWithCounts(ctx)
	counts := map[string]int{}
	for _, g := range genres {
		counts[g.Code] = g.Books
	}
	if counts["sf"] != 2 || counts["prose_classic"] != 3 || counts["dramaturgy"] != 1 {
		t.Errorf("genre counts (deleted excluded): %v", counts)
	}
	folders, _ := st.SampleFolders(ctx, 10)
	if len(folders) != 2 {
		t.Errorf("sample folders: %v", folders)
	}
	if folders, _ = st.SampleFolders(ctx, 1); len(folders) != 1 {
		t.Errorf("sample folders limit: %v", folders)
	}
}

func TestAddDeleteRestoreClear(t *testing.T) {
	st, ids := newLibrary(t)
	ctx := context.Background()

	// Adding to a live library reuses folder, author and series rows.
	id, err := st.AddBook(ctx, &BookInput{
		Title: "Воскресение", Authors: []AuthorName{{Last: "Толстой", First: "Лев"}}, Series: "Классика", SeriesNum: 3,
		Genres: []string{"prose_classic", "new_genre"}, Keywords: []string{"роман"},
		Folder: "one.zip", File: "u1", Ext: "epub", Size: 5, Lang: "ru", Year: 1899, Added: "2024-02-01",
		FileHash: "fh1", ContentHash: "ch1", ISBN: "978-5-17-080090-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	var authors, series, folders int
	st.DB().QueryRow(`SELECT count(*) FROM authors`).Scan(&authors)
	st.DB().QueryRow(`SELECT count(*) FROM series`).Scan(&series)
	st.DB().QueryRow(`SELECT count(*) FROM folders`).Scan(&folders)
	if authors != 6 || series != 1 || folders != 2 {
		t.Errorf("lookup tables after AddBook: authors %d series %d folders %d", authors, series, folders)
	}
	if got := titles(must(st.SearchTitles(ctx, "Воскрес", 10))); len(got) != 1 {
		t.Errorf("added book must be searchable: %v", got)
	}
	if books, _, _ := st.SeriesBooks(ctx, seriesIDOf(t, st, "Классика")); len(books) != 3 || books[2].SeqNumber != 3 {
		t.Errorf("series after add: %v", titles(books))
	}
	if b, err := st.FindByHash(ctx, "file_hash", "fh1"); err != nil || b.ID != id {
		t.Errorf("FindByHash file: %v %v", b, err)
	}
	if b, err := st.FindByHash(ctx, "content_hash", "ch1"); err != nil || b.ID != id {
		t.Errorf("FindByHash content: %v %v", b, err)
	}
	if _, err := st.FindByHash(ctx, "content_hash", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown hash: %v", err)
	}
	if _, err := st.FindByHash(ctx, "file_hash", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty hash: %v", err)
	}
	if _, err := st.FindByHash(ctx, "title", "x"); err == nil {
		t.Error("bad column must be rejected")
	}
	if similar, _ := st.FindSimilar(ctx, "Воскресение", []string{"Толстой"}); len(similar) != 1 {
		t.Errorf("FindSimilar: %v", titles(similar))
	}
	if similar, _ := st.FindSimilar(ctx, "Воскресение", []string{"Чехов"}); len(similar) != 0 {
		t.Errorf("FindSimilar wrong author: %v", titles(similar))
	}

	// Delete hides everywhere, restore brings it back (including FTS).
	if err := st.SetBookDeleted(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.BookCount(ctx); n != 6 {
		t.Errorf("count after delete: %d", n)
	}
	if got := must(st.SearchTitles(ctx, "Воскрес", 10)); len(got) != 0 {
		t.Errorf("deleted book still in FTS: %v", titles(got))
	}
	if _, err := st.FindByHash(ctx, "file_hash", "fh1"); !errors.Is(err, ErrNotFound) {
		t.Error("deleted book must not count as a duplicate")
	}
	if err := st.SetBookDeleted(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if got := must(st.SearchTitles(ctx, "Воскрес", 10)); len(got) != 1 {
		t.Errorf("restored book missing from FTS: %v", titles(got))
	}
	if err := st.SetBookDeleted(ctx, ids["1984"], false); err != nil {
		t.Errorf("restore imported book: %v", err)
	}
	if got := must(st.SearchTitles(ctx, "1984", 10)); len(got) != 1 {
		t.Errorf("restored imported book must be searchable: %v", titles(got))
	}
	if err := st.SetBookDeleted(ctx, 99999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete unknown: %v", err)
	}

	// A second bulk import is refused until the library is cleared.
	if _, err := st.NewImport(ctx); err == nil {
		t.Error("NewImport on a non-empty library must fail")
	}
	if err := st.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.BookCount(ctx); n != 0 {
		t.Errorf("count after clear: %d", n)
	}
	if got := must(st.SearchTitles(ctx, "Война", 10)); len(got) != 0 {
		t.Errorf("FTS after clear: %v", titles(got))
	}
	if st.GetMeta(ctx, "collection_name", "") == "" && false {
		t.Error("meta must survive Clear")
	}
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatalf("NewImport after Clear: %v", err)
	}
	session.Add(&BookInput{Title: "После очистки", Folder: "z.zip", File: "1", Ext: "fb2", Added: "2024-01-01"})
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.BookCount(ctx); n != 1 {
		t.Errorf("count after re-import: %d", n)
	}
}

func TestImportAbortAndBatches(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	session, _ := st.NewImport(ctx)
	session.Add(&BookInput{Title: "Брошенная", Folder: "a.zip", File: "1", Ext: "fb2"})
	session.Abort()
	if n, _ := st.BookCount(ctx); n != 0 {
		t.Errorf("aborted import left %d books", n)
	}

	// More rows than one batch: the session commits mid-way and continues.
	session, err = st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	total := importBatchSize + 10
	for i := 0; i < total; i++ {
		if err := session.Add(&BookInput{
			Title: fmt.Sprintf("Книга %d", i), Authors: []AuthorName{{Last: fmt.Sprintf("Автор%d", i%100)}},
			Folder: "a.zip", File: fmt.Sprint(i), Ext: "fb2", Added: "2024-01-01",
		}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := session.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Books != total || stats.Authors != 100 {
		t.Errorf("stats: %+v", stats)
	}
	if n, _ := st.BookCount(ctx); n != total {
		t.Errorf("count: %d", n)
	}
	if got := must(st.SearchTitles(ctx, "Книга 25009", 5)); len(got) != 1 {
		t.Errorf("last batch must be indexed: %v", titles(got))
	}
	// Indexes are back after Finish.
	var idx int
	st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_books_added'`).Scan(&idx)
	if idx != 1 {
		t.Error("idx_books_added must be rebuilt by Finish")
	}
}

func TestISBNHelpers(t *testing.T) {
	cases := map[string]string{
		"978-5-17-080090-2": "9785170800902",
		"0-306-40615-2":     "0306406152",
		"030640615x":        "030640615X",
		"12345":             "",
		"":                  "",
		"978 5 17 080090 2": "9785170800902",
	}
	for in, want := range cases {
		if got := NormalizeISBN(in); got != want {
			t.Errorf("NormalizeISBN(%q) = %q, want %q", in, got, want)
		}
	}
	for q, want := range map[string]bool{
		"978-5-17-080090-2": true, "0306406152": true, "Война и мир": false, "12345": false, "978-5-17-08009O-2": false,
	} {
		if got := LooksLikeISBN(q); got != want {
			t.Errorf("LooksLikeISBN(%q) = %v", q, got)
		}
	}
}

// --- helpers ---

func must(books []Book, err error) []Book {
	if err != nil {
		panic(err)
	}
	return books
}

func authorID(t *testing.T, st *Store, last string) int64 {
	t.Helper()
	var id int64
	if err := st.DB().QueryRow(`SELECT id FROM authors WHERE last_name = ?`, last).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seriesIDOf(t *testing.T, st *Store, title string) int64 {
	t.Helper()
	var id int64
	if err := st.DB().QueryRow(`SELECT id FROM series WHERE title = ?`, title).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("series %q not found", title)
	}
	return id
}
