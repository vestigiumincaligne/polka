package collections

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vestigiumincaligne/polka/internal/store"
)

const sample = `{
  "slug": "Test List",
  "title": "Тестовая подборка",
  "source": "unit",
  "items": [
    {"title": "1984", "author": "Джордж Оруэлл"},
    "Михаил Булгаков — Мастер и Маргарита",
    {"title": "Нет такой книги", "author": "Никто"},
    {"title": "   "}
  ]
}`

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []struct{ title, last, first string }{
		{"1984", "Оруэлл", "Джордж"},
		{"Мастер и Маргарита", "Булгаков", "Михаил"},
	} {
		if err := session.Add(&store.BookInput{
			Title: b.title, Authors: []store.AuthorName{{Last: b.last, First: b.first}},
			Folder: "x.zip", File: b.title, Ext: "fb2", Lang: "ru", Added: "2024-01-01",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestParse(t *testing.T) {
	f, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Slug != "test-list" {
		t.Errorf("slug = %q", f.Slug)
	}
	if len(f.Items) != 3 {
		t.Fatalf("items = %d, want 3 (blank dropped)", len(f.Items))
	}
	if f.Items[1].Author != "Михаил Булгаков" || f.Items[1].Title != "Мастер и Маргарита" {
		t.Errorf("string item parsed as %+v", f.Items[1])
	}
	if _, err := Parse(strings.NewReader(`{"title":"x","items":[]}`)); err == nil {
		t.Error("expected error for empty items")
	}
	if _, err := Parse(strings.NewReader(`{"items":["a — b"]}`)); err == nil {
		t.Error("expected error for missing title")
	}
}

func TestImportMatchAndShelf(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	cs, err := Open(filepath.Join(t.TempDir(), "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	f, _ := Parse(strings.NewReader(sample))
	c, err := cs.Import(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 3 || c.Matched != 0 {
		t.Fatalf("after import: %+v", c)
	}

	stats, err := cs.Match(ctx, st, c.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Matched != 2 || stats.Total != 3 {
		t.Fatalf("match: %+v", stats)
	}
	c, _ = cs.Get(ctx, c.Slug)
	if c.Matched != 2 || c.MatchedAt == "" {
		t.Fatalf("after match: %+v", c)
	}

	ids, err := cs.BookIDs(ctx, c.ID, 10, 0)
	if err != nil || len(ids) != 2 {
		t.Fatalf("book ids: %v %v", ids, err)
	}
	books, _ := st.BooksByIDs(ctx, ids)
	if books[0].Title != "1984" || books[1].Title != "Мастер и Маргарита" {
		t.Errorf("order not preserved: %+v", books)
	}
	if page, _ := cs.BookIDs(ctx, c.ID, 1, 1); len(page) != 1 || page[0] != ids[1] {
		t.Errorf("pagination: %v", page)
	}

	items, _ := cs.Items(ctx, c.ID)
	if items[2].BookID != 0 || items[0].MatchKind != store.MatchExact {
		t.Errorf("items: %+v", items)
	}
	of, _ := cs.CollectionsOfBook(ctx, ids[0])
	if len(of) != 1 || of[0].Slug != c.Slug {
		t.Errorf("collections of book: %+v", of)
	}

	// Re-importing the same slug replaces the list.
	f2, _ := Parse(strings.NewReader(`{"slug":"test-list","title":"Новое имя","items":["Джордж Оруэлл — 1984"]}`))
	c2, err := cs.Import(ctx, f2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.ID != c.ID || c2.Title != "Новое имя" || c2.Total != 1 || c2.Matched != 0 {
		t.Errorf("re-import: %+v", c2)
	}
	all, err := cs.MatchAll(ctx, st)
	if err != nil || len(all) != 1 || all[0].Matched != 1 {
		t.Errorf("match all: %+v %v", all, err)
	}
	if cs.Count(ctx) != 1 {
		t.Error("count")
	}
	if err := cs.Delete(ctx, "test-list"); err != nil {
		t.Fatal(err)
	}
	if err := cs.Delete(ctx, "test-list"); err != ErrNotFound {
		t.Errorf("delete missing: %v", err)
	}
}

func TestSeedBundled(t *testing.T) {
	ctx := context.Background()
	cs, err := Open(filepath.Join(t.TempDir(), "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	fsys := fstest.MapFS{
		"a.json":    {Data: []byte(`{"slug":"a","title":"A","items":["X — Y"]}`)},
		"b.json":    {Data: []byte(`{"slug":"b","title":"B","items":["X — Y"]}`)},
		"readme.md": {Data: []byte("ignored")},
	}
	seeded, err := cs.SeedBundled(ctx, fsys)
	if err != nil || len(seeded) != 2 {
		t.Fatalf("seed: %v %v", seeded, err)
	}
	a, _ := cs.Get(ctx, "a")
	if !a.Bundled() {
		t.Error("a should be bundled")
	}
	// A deleted bundled collection does not come back; a manual one under the same slug is not overwritten.
	if err := cs.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	f, _ := Parse(strings.NewReader(`{"slug":"b","title":"Моя B","items":["X — Y","Z — W"]}`))
	if _, err := cs.Import(ctx, f); err != nil {
		t.Fatal(err)
	}
	seeded, err = cs.SeedBundled(ctx, fsys)
	if err != nil || len(seeded) != 0 {
		t.Fatalf("re-seed: %v %v", seeded, err)
	}
	if _, err := cs.Get(ctx, "a"); err != ErrNotFound {
		t.Error("a should stay hidden")
	}
	b, _ := cs.Get(ctx, "b")
	if b.Bundled() || b.Title != "Моя B" || b.Total != 2 {
		t.Errorf("manual b overwritten: %+v", b)
	}
	// An updated bundled collection file is applied.
	fsys["c.json"] = &fstest.MapFile{Data: []byte(`{"slug":"c","title":"C","items":["X — Y"]}`)}
	if seeded, _ = cs.SeedBundled(ctx, fsys); len(seeded) != 1 || seeded[0] != "c" {
		t.Errorf("new bundled: %v", seeded)
	}
}
