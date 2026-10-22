package server

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vestigiumincaligne/polka/internal/store"
)

func TestBookSeriesLazyArchiveEnrichment(t *testing.T) {
	ts, client, st, dir := newManageServerDir(t)
	archive, err := os.Create(filepath.Join(dir, "lib", "books.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(archive)
	entry, err := zw.Create("1.fb2")
	if err != nil {
		t.Fatal(err)
	}
	book := strings.Replace(readerFB2, "</title-info>", `
		<sequence name="Original" number="7"/>
		<sequence name="Extra" number="2"/>
		</title-info>`, 1)
	if _, err := entry.Write([]byte(book)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	id, err := st.AddBook(t.Context(), &store.BookInput{Title: "Archive book", Series: "Original", SeriesNum: 7, Folder: "books.zip", File: "1", Ext: "fb2"})
	if err != nil {
		t.Fatal(err)
	}
	form := getJSONWith(t, client, ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(id))
	series := form["series"].([]any)
	if len(series) != 2 || series[0].(map[string]any)["SeriesTitle"] != "Original" || series[1].(map[string]any)["SeriesTitle"] != "Extra" || series[1].(map[string]any)["SeqNumber"] != float64(2) {
		t.Fatalf("archive detail did not expose added memberships: %v", form["series"])
	}
	getJSONWith(t, client, ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(id))
	refs, err := st.BookSeries(t.Context(), id)
	if err != nil || len(refs) != 2 || refs[0].SeqNumber != 7 {
		t.Fatalf("repeat archive enrichment changed memberships: %+v, %v", refs, err)
	}
	missingID, err := st.AddBook(t.Context(), &store.BookInput{Title: "Missing archive", Series: "Kept", SeriesNum: 5, Folder: "missing.zip", File: "2", Ext: "fb2"})
	if err != nil {
		t.Fatal(err)
	}
	missing := getJSONWith(t, client, ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(missingID))["series"].([]any)
	if len(missing) != 1 || missing[0].(map[string]any)["SeqNumber"] != float64(5) {
		t.Fatalf("missing archive erased existing series: %v", missing)
	}
}

func TestBookSeriesFromFB2(t *testing.T) {
	ts, client, st := newManageServer(t)
	book := strings.Replace(readerFB2, "</title-info>", `
		<sequence name="First"/>
		<sequence name="Second" number="7"><sequence name="Nested" number="2"/></sequence>
		</title-info>`, 1)
	var up map[string][]uploadResult
	decode(t, uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{"series.fb2": []byte(book)}, nil), &up)
	id := up["results"][0].BookID
	refs, err := st.BookSeries(t.Context(), id)
	if err != nil || len(refs) != 3 {
		t.Fatalf("upload lost series: %+v, %v", refs, err)
	}
	// Simulate the primary-only membership backfilled for an existing catalog.
	if _, err := st.DB().Exec(`DELETE FROM book_series WHERE book_id = ? AND series_id != ?`, id, refs[0].ID); err != nil {
		t.Fatal(err)
	}
	form := getJSONWith(t, client, ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(id))
	data, err := json.Marshal(form["series"])
	if err != nil {
		t.Fatal(err)
	}
	var series []struct {
		SeriesID    int64
		SeriesTitle string
		SeqNumber   int
	}
	if err := json.Unmarshal(data, &series); err != nil {
		t.Fatal(err)
	}
	if len(series) != 3 || series[0].SeriesTitle != "First" || series[0].SeqNumber != 0 || series[1].SeqNumber != 7 || series[2].SeqNumber != 2 {
		t.Fatalf("book view lost series/numbers: %+v", series)
	}
	for _, ref := range series {
		page := getJSONWith(t, client, ts.URL+"/main/getBooks/getSearchSeriesBooks?selectedItemID="+strconv.FormatInt(ref.SeriesID, 10))
		books := page["titlesList"].([]any)
		if page["title"] != ref.SeriesTitle || len(books) != 1 || books[0].(map[string]any)["SeqNumber"] != float64(ref.SeqNumber) {
			t.Fatalf("series link %s returned wrong membership: %v", ref.SeriesTitle, page)
		}
	}
}
