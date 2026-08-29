package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vestigiumincaligne/polka/internal/collections"
	"github.com/vestigiumincaligne/polka/internal/store"
)

func TestCollectionsAPI(t *testing.T) {
	ts, client, st := newManageServer(t)
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Add(&store.BookInput{
		Title: "Война и мир", Authors: []store.AuthorName{{Last: "Толстой", First: "Лев"}},
		Folder: "arch-001.zip", File: "100", Ext: "fb2", Lang: "ru", Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	var bookID int64
	st.DB().QueryRow(`SELECT id FROM books`).Scan(&bookID)

	// Import via multipart (as from the browser).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "forbes.json")
	fw.Write([]byte(`{"slug":"forbes","title":"Forbes 100","items":[
		{"title":"Война и мир","author":"Лев Толстой"},
		"Джордж Оруэлл — 1984"]}`))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/admin/collections/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import -> %d", resp.StatusCode)
	}
	getJSON := func(t *testing.T, url string) map[string]any {
		return getJSONWith(t, client, url)
	}

	list := getJSON(t, ts.URL+"/api/v1/collections")
	// Besides the uploaded one there are bundled collections; find ours.
	var c map[string]any
	for _, x := range list["collections"].([]any) {
		if m := x.(map[string]any); m["slug"] == "forbes" {
			c = m
		}
	}
	if c == nil {
		t.Fatalf("collections: %v", list["collections"])
	}
	if c["bundled"] != false || c["matched"].(float64) != 1 || c["total"].(float64) != 2 || c["shelfId"] != "collection_forbes" {
		t.Errorf("collection: %v", c)
	}

	one := getJSON(t, ts.URL+"/api/v1/collections/forbes")
	items := one["items"].([]any)
	first := items[0].(map[string]any)
	if first["book"] == nil || first["book"].(map[string]any)["BookID"].(float64) != float64(bookID) {
		t.Errorf("first item should be matched: %v", first)
	}
	if items[1].(map[string]any)["book"] != nil {
		t.Errorf("second item should be unmatched: %v", items[1])
	}

	// Home page shelf and paginated output.
	home := getJSON(t, ts.URL+"/main/getBooks/getHomeShelves")
	found := false
	for _, sh := range home["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "collection_forbes" {
			found = true
			if !strings.Contains(m["subtitle"].(string), "1 из 2") {
				t.Errorf("subtitle: %v", m["subtitle"])
			}
		}
	}
	if !found {
		t.Error("collection shelf missing on home")
	}
	shelf := getJSON(t, ts.URL+"/main/getBooks/getShelfBooks?shelfId=collection_forbes")
	if shelf["title"] != "Forbes 100" || len(shelf["titlesList"].([]any)) != 1 {
		t.Errorf("shelf: %v", shelf)
	}

	// Rematch and delete.
	for _, p := range []string{"/admin/collections/forbes/match", "/admin/collections/forbes/delete"} {
		resp, err := client.Post(ts.URL+p, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s -> %d", p, resp.StatusCode)
		}
	}
	resp, _ = client.Get(ts.URL + "/api/v1/collections/forbes")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete -> %d", resp.StatusCode)
	}
}

func getJSONWith(t *testing.T, client *http.Client, url string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Polka-Lang", "ru")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s -> %d", url, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCollectionSourcesSetting(t *testing.T) {
	ts, client, st, dir := newManageServerDir(t)
	ctx := context.Background()
	session, _ := st.NewImport(ctx)
	session.Add(&store.BookInput{
		Title: "Внутренняя сила", Authors: []store.AuthorName{{Last: "Нефф", First: "Кристин"}},
		Folder: "a.zip", File: "1", Ext: "fb2", Lang: "ru", Added: "2024-01-01",
	})
	session.Finish()

	// A collection "from Forbes" in the database (as if the source had already loaded it).
	cs, err := collections.Open(filepath.Join(dir, "collections.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	f, _ := collections.Parse(strings.NewReader(`{"slug":"forbes-1","title":"Семь книг","items":["Кристин Нефф — Внутренняя сила"]}`))
	if _, err := cs.ImportFrom(ctx, f, "forbes"); err != nil {
		t.Fatal(err)
	}
	cs.Match(ctx, st, "forbes-1")

	hasShelf := func() bool {
		home := getJSONWith(t, client, ts.URL+"/main/getBooks/getHomeShelves")
		for _, sh := range home["shelves"].([]any) {
			if sh.(map[string]any)["id"] == "collection_forbes-1" {
				return true
			}
		}
		return false
	}
	// The source is disabled by default: no shelf, enabled=false in settings.
	if hasShelf() {
		t.Error("disabled source shelf should be hidden")
	}
	settings := getJSONWith(t, client, ts.URL+"/admin/settings")
	src := settings["collectionSources"].([]any)[0].(map[string]any)
	if src["id"] != "forbes" || src["enabled"] != false {
		t.Errorf("settings: %v", src)
	}
	resp, _ := client.Post(ts.URL+"/admin/collections/sources/forbes/sync", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("sync of disabled source -> %d", resp.StatusCode)
	}

	// Enable it: the shelf appears.
	resp = postJSON(t, client, ts.URL+"/admin/settings", map[string]any{"collectionSources": map[string]bool{"forbes": true}})
	resp.Body.Close()
	if !hasShelf() {
		t.Error("enabled source shelf should be visible")
	}
}
