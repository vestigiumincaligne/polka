package importer

import (
	"archive/zip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vestigiumincaligne/polka/internal/store"
)

const sep = "\x04"

// makeInpx builds a synthetic inpx: records are lines already containing \x04 fields.
func makeInpx(t testing.TB, dir string, structure string, inps map[string][]string) string {
	t.Helper()
	path := filepath.Join(dir, "test.inpx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	if structure != "" {
		w, _ := zw.Create("structure.info")
		w.Write([]byte(structure))
	}
	w, _ := zw.Create("collection.info")
	w.Write([]byte("Тестовая коллекция\nописание"))
	for name, lines := range inps {
		w, _ := zw.Create(name)
		w.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func record(author, genre, title, series, serno, file, ext string) string {
	return strings.Join([]string{
		author, genre, title, series, serno, file, "1000", "1", "0", ext,
		"2024-01-02", "ru", "4", "ключ:слово", "2020", "src",
	}, sep)
}

func TestImportInpx(t *testing.T) {
	dir := t.TempDir()
	inpxPath := makeInpx(t, dir, "", map[string][]string{
		"arch-001.inp": {
			record("Пушкин,Александр,Сергеевич", "poetry", "Евгений Онегин", "Классика", "1", "f1", "fb2"),
			record("Пушкин,Александр,Сергеевич", "poetry:prose_classic", "Капитанская дочка", "Классика", "2", "f2", "fb2"),
		},
		"arch-002.inp": {
			record("Лермонтов,Михаил,", "prose_classic", "Герой нашего времени", "", "", "f3", "epub"),
		},
	})

	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	stats, err := ImportInpx(context.Background(), log, st, inpxPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Books != 3 || stats.Authors != 2 || stats.Series != 1 || stats.Genres != 2 {
		t.Errorf("stats = %+v", stats)
	}

	ctx := context.Background()
	if n, _ := st.BookCount(ctx); n != 3 {
		t.Errorf("BookCount = %d, want 3", n)
	}
	if name := st.GetMeta(ctx, "collection_name", ""); name != "Тестовая коллекция" {
		t.Errorf("collection_name = %q", name)
	}

	// Folder derived from the .inp name
	var folder string
	if err := st.DB().QueryRow(
		`SELECT f.name FROM books b JOIN folders f ON f.id = b.folder_id WHERE b.title = ?`,
		"Герой нашего времени").Scan(&folder); err != nil {
		t.Fatal(err)
	}
	if folder != "arch-002.zip" {
		t.Errorf("folder = %q, want arch-002.zip", folder)
	}

	// FTS search
	var found int
	if err := st.DB().QueryRow(
		`SELECT count(*) FROM book_search WHERE book_search MATCH ?`, "пушкин").Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 2 {
		t.Errorf("FTS match for author = %d, want 2", found)
	}

	// A repeated import without --replace must fail
	if _, err := ImportInpx(context.Background(), log, st, inpxPath, nil); err == nil {
		t.Error("reimport into non-empty library must fail")
	}
}

// TestImportScale is a scale test: importing a large synthetic catalog.
// Run explicitly: POLKA_SCALE_BOOKS=500000 go test -run Scale -v ./internal/importer/
func TestImportScale(t *testing.T) {
	countStr := os.Getenv("POLKA_SCALE_BOOKS")
	if countStr == "" {
		t.Skip("set POLKA_SCALE_BOOKS to run")
	}
	var total int
	fmt.Sscan(countStr, &total)

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "scale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Abort()

	genres := []string{"sf", "sf_fantasy", "detective", "prose_classic", "love_short", "adv_history", "poetry", "nonf_biography"}
	start := time.Now()
	for i := 0; i < total; i++ {
		b := &store.BookInput{
			LibID: fmt.Sprint(i),
			Title: fmt.Sprintf("Книга номер %d о приключениях и тайнах", i),
			Authors: []store.AuthorName{{
				Last:  fmt.Sprintf("Автор%d", i%120_000),
				First: fmt.Sprintf("Имя%d", i%97),
			}},
			Genres:   []string{genres[i%len(genres)], genres[(i*7)%len(genres)]},
			Folder:   fmt.Sprintf("fb2-%06d.zip", i/1000),
			File:     fmt.Sprint(i),
			Ext:      "fb2",
			Size:     int64(i%900_000 + 10_000),
			Lang:     "ru",
			Added:    "2024-06-01",
			Rate:     float64(i % 6),
			Keywords: []string{fmt.Sprintf("тег%d", i%5000)},
		}
		if i%3 != 0 {
			b.Series = fmt.Sprintf("Серия %d", i%60_000)
			b.SeriesNum = i % 40
		}
		if err := session.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	loadDur := time.Since(start)

	stats, err := session.Finish()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("books=%d load=%s total=%s (%.0f books/sec)",
		stats.Books, loadDur.Round(time.Millisecond), stats.Duration.Round(time.Millisecond),
		float64(stats.Books)/stats.Duration.Seconds())

	// Search after import must be fast.
	for _, q := range []string{"приключениях", "Автор12345", "тайнах номер"} {
		qStart := time.Now()
		var n int
		if err := st.DB().QueryRow(`SELECT count(*) FROM book_search WHERE book_search MATCH ?`, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("FTS %q: %d rows in %s", q, n, time.Since(qStart).Round(time.Microsecond))
	}

	// Timings of the main API requests on the full database.
	timeIt := func(name string, fn func() error) {
		qStart := time.Now()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("query %-20s %s", name, time.Since(qStart).Round(time.Microsecond))
	}
	timeIt("HomeShelves", func() error { _, err := st.HomeShelves(ctx, 12); return err })
	timeIt("CatalogShelves", func() error { _, err := st.CatalogShelves(ctx, 6, 12); return err })
	timeIt("SearchTitles", func() error { _, err := st.SearchTitles(ctx, "приключениях", 250); return err })
	timeIt("SearchAuthors", func() error { _, err := st.SearchAuthors(ctx, "Автор123", 250); return err })
	timeIt("SearchStats", func() error { _, err := st.SearchStats(ctx, "тайнах"); return err })
	timeIt("ShelfBooks p10", func() error { _, _, err := st.ShelfBooks(ctx, "newest", 60, 600); return err })
	timeIt("AuthorBooks", func() error { _, _, err := st.AuthorBooks(ctx, 50); return err })

	var dbSize int64
	if fi, err := os.Stat(filepath.Join(dir, "scale.db")); err == nil {
		dbSize = fi.Size()
	}
	t.Logf("db size: %d MB", dbSize/1024/1024)
}
