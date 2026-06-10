package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

const isbnFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description><title-info>
<genre>sf_fantasy</genre>
<author><first-name>Анна</first-name><last-name>Исбнова</last-name></author>
<book-title>Книга с ISBN</book-title><lang>ru</lang>
</title-info>
<publish-info><isbn>978-5-389-01686-6</isbn></publish-info>
</description>
<body><section><p>Текст.</p></section></body>
</FictionBook>`

func TestSearchByISBNAndGenres(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"isbn.fb2": []byte(isbnFB2),
	}, nil)
	resp.Body.Close()

	// Search by ISBN (in various spellings)
	for _, q := range []string{"978-5-389-01686-6", "9785389016866", "978 5 389 01686 6"} {
		var out map[string][]map[string]any
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/main/getBooks/getSearchTitles?search="+url.QueryEscape(q), nil)
		req.Header.Set("X-Polka-Lang", "ru")
		resp, _ := client.Do(req)
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if len(out["titlesList"]) != 1 || out["titlesList"][0]["Title"] != "Книга с ISBN" {
			t.Errorf("isbn search %q = %v", q, out["titlesList"])
		}
	}

	// Stats for an ISBN query
	var stats map[string]map[string]float64
	resp, _ = client.Get(ts.URL + "/main/getBooks/getSearchStats?search=9785389016866")
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["searchStats"]["bookTitles"] != 1 {
		t.Errorf("isbn stats = %v", stats)
	}

	// Unknown ISBN: external resolution will fail (no network in tests) -> empty, not 500
	resp, _ = client.Get(ts.URL + "/main/getBooks/getSearchTitles?search=9780000000002")
	var empty map[string][]map[string]any
	json.NewDecoder(resp.Body).Decode(&empty)
	if resp.StatusCode != 200 || len(empty["titlesList"]) != 0 {
		t.Errorf("unknown isbn -> %d, %v", resp.StatusCode, empty["titlesList"])
	}
	resp.Body.Close()

	// Genre search by Russian name
	var genres map[string][]map[string]any
	reqG, _ := http.NewRequest(http.MethodGet, ts.URL+"/main/getBooks/getSearchGenres?search=фэнтези", nil)
	reqG.Header.Set("X-Polka-Lang", "ru")
	resp, _ = client.Do(reqG)
	json.NewDecoder(resp.Body).Decode(&genres)
	resp.Body.Close()
	if len(genres["genresList"]) != 1 || genres["genresList"][0]["GenreCode"] != "sf_fantasy" {
		t.Errorf("genre search = %v", genres["genresList"])
	}

	// Genres in regular query statistics
	reqS, _ := http.NewRequest(http.MethodGet, ts.URL+"/main/getBooks/getSearchStats?search=фэнтези", nil)
	reqS.Header.Set("X-Polka-Lang", "ru")
	resp, _ = client.Do(reqS)
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["searchStats"]["genres"] != 1 {
		t.Errorf("genre stats = %v", stats)
	}

	// A genre shelf opens by code
	var shelf map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getShelfBooks?shelfId=genre_sf_fantasy")
	json.NewDecoder(resp.Body).Decode(&shelf)
	resp.Body.Close()
	if len(shelf["titlesList"].([]any)) != 1 {
		t.Errorf("genre shelf = %v", shelf["titlesList"])
	}
}
