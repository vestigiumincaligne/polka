package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const opdsFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description><title-info>
<genre>prose_classic</genre>
<author><first-name>Лев</first-name><last-name>Толстой</last-name></author>
<book-title>Война и мир</book-title>
<lang>ru</lang>
</title-info></description>
<body><section><p>Текст.</p></section></body>
</FictionBook>`

func TestOpdsCatalog(t *testing.T) {
	ts, client, _ := newManageServer(t)

	// A book in the library
	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"voyna.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()

	// Without authentication — 401 with a Basic challenge
	plain := &http.Client{}
	resp, _ = plain.Get(ts.URL + "/opds")
	resp.Body.Close()
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("opds without auth -> %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	get := func(path string) string {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Accept-Language", "ru") // feed titles asserted in Russian
		req.SetBasicAuth("admin", "secret123")
		resp, err := plain.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s -> %d", path, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	// Root: navigation
	root := get("/opds")
	for _, want := range []string{"<feed", "Новинки", "/opds/new", "/opds/genres", "Читаю сейчас"} {
		if !strings.Contains(root, want) {
			t.Errorf("root feed missing %q", want)
		}
	}

	// New arrivals: a book with download and cover links
	news := get("/opds/new")
	for _, want := range []string{
		"Война и мир",
		"Толстой Лев",
		"application/fb2+zip",
		"/Images/covers/",
		"http://opds-spec.org/acquisition",
	} {
		if !strings.Contains(news, want) {
			t.Errorf("new feed missing %q in:\n%s", want, news[:min(len(news), 800)])
		}
	}

	// Genres and books of a genre
	genresFeed := get("/opds/genres")
	if !strings.Contains(genresFeed, "Классическая проза") || !strings.Contains(genresFeed, "/opds/genre/prose_classic") {
		t.Errorf("genres feed: %s", genresFeed[:min(len(genresFeed), 500)])
	}
	genreFeed := get("/opds/genre/prose_classic")
	if !strings.Contains(genreFeed, "Война и мир") {
		t.Error("genre feed missing book")
	}

	// Search
	searchFeed := get("/opds/search?q=толстой")
	if !strings.Contains(searchFeed, "Война и мир") {
		t.Error("search feed missing book")
	}

	// Download with Basic authentication (the way readers do it)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/Images/fb2/"+itoa64(up["results"][0].BookID), nil)
	req.SetBasicAuth("admin", "secret123")
	resp, err := plain.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("basic-auth download -> %d", resp.StatusCode)
	}

	// OpenSearch
	osd := get("/opds/opensearch")
	if !strings.Contains(osd, "{searchTerms}") {
		t.Error("opensearch descriptor broken")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
