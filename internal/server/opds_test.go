package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
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

	// Книга в библиотеке
	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"voyna.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()

	// Без аутентификации — 401 с приглашением Basic
	plain := &http.Client{}
	resp, _ = plain.Get(ts.URL + "/opds")
	resp.Body.Close()
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("opds without auth -> %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	get := func(path string) string {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Accept-Language", "ru") // заголовки фидов проверяются по-русски
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

	// Корень: навигация
	root := get("/opds")
	for _, want := range []string{"<feed", "Новинки", "/opds/new", "/opds/genres", "Читаю сейчас"} {
		if !strings.Contains(root, want) {
			t.Errorf("root feed missing %q", want)
		}
	}

	// Новинки: книга с ссылками на скачивание и обложку
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

	// Жанры и книги жанра
	genresFeed := get("/opds/genres")
	if !strings.Contains(genresFeed, "Классическая проза") || !strings.Contains(genresFeed, "/opds/genre/prose_classic") {
		t.Errorf("genres feed: %s", genresFeed[:min(len(genresFeed), 500)])
	}
	genreFeed := get("/opds/genre/prose_classic")
	if !strings.Contains(genreFeed, "Война и мир") {
		t.Error("genre feed missing book")
	}

	// Поиск
	searchFeed := get("/opds/search?q=толстой")
	if !strings.Contains(searchFeed, "Война и мир") {
		t.Error("search feed missing book")
	}

	// Скачивание с Basic-аутентификацией (как делают читалки)
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

func TestOpdsAuthorsSeriesAndToggle(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"voyna.fb2": []byte(opdsFB2),
	}, nil)
	resp.Body.Close()

	get := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Accept-Language", "ru")
		req.SetBasicAuth("admin", "secret123")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// Root lists author/series navigation
	_, root := get("/opds")
	for _, want := range []string{"По авторам", "По сериям", "/opds/authors", "/opds/series"} {
		if !strings.Contains(root, want) {
			t.Errorf("root feed missing %q", want)
		}
	}

	// Author letter "Т" (opdsFB2 author last name starts with Т — Толстой)
	code, letterFeed := get("/opds/authors/" + url.PathEscape("Т"))
	if code != 200 || !strings.Contains(letterFeed, "/opds/author/") {
		t.Fatalf("authors letter -> %d, body has author link: %v", code, strings.Contains(letterFeed, "/opds/author/"))
	}
	m := regexp.MustCompile(`/opds/author/(\d+)`).FindStringSubmatch(letterFeed)
	if m == nil {
		t.Fatal("no author id in letter feed")
	}
	_, authorBooks := get("/opds/author/" + m[1])
	if !strings.Contains(authorBooks, "opds-spec.org/acquisition") {
		t.Error("author feed has no acquisition links")
	}

	// Letters are valid OPDS navigation
	if code, _ := get("/opds/series"); code != 200 {
		t.Errorf("series letters -> %d", code)
	}

	// Toggle off -> 404, on -> 200 (client is an authed admin session)
	postJSON(t, client, ts.URL+"/admin/settings", map[string]any{"opdsEnabled": false}).Body.Close()
	if code, _ := get("/opds"); code != 404 {
		t.Errorf("opds after disable -> %d, want 404", code)
	}
	postJSON(t, client, ts.URL+"/admin/settings", map[string]any{"opdsEnabled": true}).Body.Close()
	if code, _ := get("/opds"); code != 200 {
		t.Errorf("opds after enable -> %d, want 200", code)
	}
}
