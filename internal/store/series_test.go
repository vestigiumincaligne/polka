package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMultipleSeries(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		name := "upload"
		if bulk {
			name = "bulk"
		}
		t.Run(name, func(t *testing.T) {
			st, err := Open(filepath.Join(t.TempDir(), "books.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			ctx := context.Background()
			books := []*BookInput{
				{Title: "First", Folder: "books", File: "1", Ext: "fb2", Series: "Primary", SeriesNum: 1,
					SeriesList: []SeriesRef{{Title: "Primary", SeqNumber: 1}, {Title: "Secondary", SeqNumber: 9}}},
				{Title: "Second", Folder: "books", File: "2", Ext: "fb2", Series: "Other", SeriesNum: 2,
					SeriesList: []SeriesRef{{Title: "Secondary", SeqNumber: 3}}},
			}
			if bulk {
				im, err := st.NewImport(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, book := range books {
					if err := im.Add(book); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := im.Finish(); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, book := range books {
					if _, err := st.AddBook(ctx, book); err != nil {
						t.Fatal(err)
					}
				}
			}
			matches, err := st.SearchBooks(ctx, "Secondary", 10, 0)
			if err != nil || len(matches) != 2 {
				t.Fatalf("secondary title search: %+v, %v", matches, err)
			}
			series, err := st.SearchSeries(ctx, "Secondary", 10)
			if err != nil || len(series) != 1 || series[0].Books != 2 {
				t.Fatalf("series counts: %+v, %v", series, err)
			}
			members, title, err := st.SeriesBooks(ctx, series[0].ID)
			if err != nil || title != "Secondary" || len(members) != 2 {
				t.Fatalf("series page: %+v, %s, %v", members, title, err)
			}
			if members[0].Title != "Second" || members[0].SeqNumber != 3 || members[1].SeqNumber != 9 || members[0].SeriesTitle != "Secondary" {
				t.Fatalf("membership-specific ordering/numbers: %+v", members)
			}
			d, err := st.BookDetails(ctx, members[1].ID)
			if err != nil || len(d.Series) != 2 || d.Series[0].Title != "Primary" || d.Series[1].Title != "Secondary" {
				t.Fatalf("book series: %+v, %v", d, err)
			}
			if err := st.SetBookDeleted(ctx, d.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := st.SetBookDeleted(ctx, d.ID, false); err != nil {
				t.Fatal(err)
			}
			matches, err = st.SearchBooks(ctx, "Secondary", 10, 0)
			if err != nil || len(matches) != 2 {
				t.Fatalf("restored search: %+v, %v", matches, err)
			}
			if err := st.Clear(ctx); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := st.db.QueryRow(`SELECT count(*) FROM book_series`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("clear: %d, %v", n, err)
			}
		})
	}
}

func TestMigrateV4SeriesAndEnrichment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "books.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := st.AddBook(ctx, &BookInput{Title: "Legacy", Series: "Original", SeriesNum: 7, Folder: "books", File: "1", Ext: "fb2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TABLE book_series; PRAGMA user_version = 4`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	refs, err := st.BookSeries(ctx, id)
	if err != nil || len(refs) != 1 || refs[0].Title != "Original" || refs[0].SeqNumber != 7 {
		t.Fatalf("v4 series backfill: %+v, %v", refs, err)
	}
	added := []SeriesRef{{Title: "Original"}, {Title: "Extra", SeqNumber: 2}}
	if err := st.AddBookSeries(ctx, id, added); err != nil {
		t.Fatal(err)
	}
	if err := st.AddBookSeries(ctx, id, added); err != nil {
		t.Fatal(err)
	}
	refs, err = st.BookSeries(ctx, id)
	if err != nil || len(refs) != 2 || refs[0].SeqNumber != 7 || refs[1].SeqNumber != 2 {
		t.Fatalf("repeat enrichment erased or duplicated membership: %+v, %v", refs, err)
	}
	var version int
	if err := st.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version: %d, %v", version, err)
	}
}
