package store

import (
	"context"
	"path/filepath"
	"testing"
)

func updateFixture(t *testing.T) (*Store, map[string]int64) {
	t.Helper()
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	im, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []*BookInput{
		{LibID: "1", Title: "Same", Authors: []AuthorName{{Last: "Толстой", First: "Лев"}}, Genres: []string{"prose"},
			Keywords: []string{"war", "peace"}, Folder: "a.zip", File: "1", Ext: "fb2", Size: 10, Lang: "ru", Added: "2024-01-01"},
		{LibID: "2", Title: "Old title", Authors: []AuthorName{{Last: "Ильф", First: "Илья"}}, Genres: []string{"humor"},
			Folder: "a.zip", File: "2", Ext: "fb2", Size: 20, Lang: "ru", Added: "2024-01-01"},
		{LibID: "3", Title: "Moved", Series: "Цикл", SeriesNum: 2, Folder: "a.zip", File: "3", Ext: "fb2", Size: 30},
	} {
		if err := im.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := im.Finish(); err != nil {
		t.Fatal(err)
	}

	ids := map[string]int64{}
	rows, err := st.DB().Query(`SELECT lib_id, id FROM books`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var lib string
		var id int64
		rows.Scan(&lib, &id)
		ids[lib] = id
	}
	return st, ids
}

func newerCatalog() []*BookInput {
	return []*BookInput{
		// unchanged (lists in another order must still match)
		{LibID: "1", Title: "Same", Authors: []AuthorName{{Last: "Толстой", First: "Лев"}}, Genres: []string{"prose"},
			Keywords: []string{"peace", "war"}, Folder: "a.zip", File: "1", Ext: "fb2", Size: 10, Lang: "ru", Added: "2024-01-01"},
		// metadata changed, author replaced
		{LibID: "2", Title: "New title", Authors: []AuthorName{{Last: "Петров", First: "Евгений"}}, Genres: []string{"humor"},
			Folder: "a.zip", File: "2", Ext: "fb2", Size: 20, Lang: "ru", Added: "2024-01-01"},
		// same lib_id, another archive
		{LibID: "3", Title: "Moved", Series: "Цикл", SeriesNum: 2, Folder: "b.zip", File: "3", Ext: "fb2", Size: 30},
		// new book
		{LibID: "4", Title: "Brand new", Authors: []AuthorName{{Last: "Новый", First: "Автор"}}, Folder: "b.zip", File: "4", Ext: "fb2", Size: 40},
	}
}

func runUpdate(t *testing.T, st *Store, books []*BookInput, dry bool) UpdateStats {
	t.Helper()
	u, err := st.NewUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer u.Abort()
	for _, b := range books {
		if err := u.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	var stats UpdateStats
	if dry {
		stats, err = u.Rollback()
	} else {
		stats, err = u.Finish()
	}
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestUpdateKeepsIDs(t *testing.T) {
	ctx := context.Background()
	st, ids := updateFixture(t)

	stats := runUpdate(t, st, newerCatalog(), false)
	if stats.Unchanged != 1 || stats.Updated != 1 || stats.Moved != 1 || stats.Added != 1 {
		t.Fatalf("stats = %+v", stats)
	}

	var count int
	st.DB().QueryRow(`SELECT count(*) FROM books`).Scan(&count)
	if count != 4 {
		t.Fatalf("books = %d, want 4", count)
	}
	for lib, id := range ids {
		var got string
		if err := st.DB().QueryRow(`SELECT lib_id FROM books WHERE id = ?`, id).Scan(&got); err != nil || got != lib {
			t.Errorf("id %d: lib_id = %q, %v; want %q", id, got, err, lib)
		}
	}

	// The replaced author (Ильф) has no books left and is removed.
	if stats.Orphans != 1 {
		t.Errorf("orphans = %d, want 1", stats.Orphans)
	}
	var ilf int
	st.DB().QueryRow(`SELECT count(*) FROM authors WHERE last_name = 'Ильф'`).Scan(&ilf)
	if ilf != 0 {
		t.Errorf("orphan author kept")
	}

	d, err := st.BookDetails(ctx, ids["2"])
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "New title" || d.AuthorNames != "Петров Евгений" {
		t.Errorf("updated book = %q / %q", d.Title, d.AuthorNames)
	}

	moved, err := st.BookDetails(ctx, ids["3"])
	if err != nil || moved.Folder != "b.zip" || moved.SeriesTitle != "Цикл" || moved.SeqNumber != 2 {
		t.Errorf("moved book = %+v, %v", moved, err)
	}

	var newID int64
	st.DB().QueryRow(`SELECT id FROM books WHERE lib_id = '4'`).Scan(&newID)
	for _, id := range ids {
		if newID <= id {
			t.Errorf("new book id %d reuses or precedes an old id %d", newID, id)
		}
	}

	// The search index follows the update.
	if got, _ := st.SearchTitles(ctx, "new title", 10); len(got) != 1 || got[0].ID != ids["2"] {
		t.Errorf("search new title = %v", titles(got))
	}
	if got, _ := st.SearchTitles(ctx, "old title", 10); len(got) != 0 {
		t.Errorf("old title still found: %v", titles(got))
	}
	if got, _ := st.SearchTitles(ctx, "brand", 10); len(got) != 1 {
		t.Errorf("new book not found: %v", titles(got))
	}

	// A second run with the same catalog changes nothing.
	if again := runUpdate(t, st, newerCatalog(), false); again.Unchanged != 4 || again.Added+again.Updated+again.Moved != 0 {
		t.Errorf("second run = %+v", again)
	}
}

func TestUpdateDryRunWritesNothing(t *testing.T) {
	st, ids := updateFixture(t)

	stats := runUpdate(t, st, newerCatalog(), true)
	if stats.Unchanged != 1 || stats.Updated != 1 || stats.Moved != 1 || stats.Added != 1 {
		t.Fatalf("dry-run stats = %+v", stats)
	}
	var count int
	st.DB().QueryRow(`SELECT count(*) FROM books`).Scan(&count)
	var title string
	st.DB().QueryRow(`SELECT title FROM books WHERE id = ?`, ids["2"]).Scan(&title)
	if count != 3 || title != "Old title" {
		t.Errorf("dry run changed the catalog: books=%d title=%q", count, title)
	}
}

func TestUpdateDuplicateLibIDNotMergedByLibID(t *testing.T) {
	st, _ := updateFixture(t)
	// Two catalog rows share lib_id "9"; a record in a new place must not
	// be glued to either of them.
	extra := []*BookInput{
		{LibID: "9", Title: "Dup A", Folder: "c.zip", File: "9", Ext: "fb2"},
		{LibID: "9", Title: "Dup B", Folder: "d.zip", File: "9", Ext: "fb2"},
	}
	runUpdate(t, st, extra, false)
	stats := runUpdate(t, st, []*BookInput{{LibID: "9", Title: "Dup C", Folder: "e.zip", File: "9", Ext: "fb2"}}, false)
	if stats.Added != 1 || stats.Moved != 0 {
		t.Errorf("stats = %+v", stats)
	}
}
