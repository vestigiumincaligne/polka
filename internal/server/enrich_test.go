package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func newJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestSettingsAndRatings(t *testing.T) {
	ts, client, _ := newManageServer(t)

	// A book
	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"r.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	bookID := itoa64(up["results"][0].BookID)

	// Default settings: everything enabled
	var settings map[string]map[string]bool
	resp, _ = client.Get(ts.URL + "/admin/settings")
	json.NewDecoder(resp.Body).Decode(&settings)
	resp.Body.Close()
	if !settings["enrichment"]["livelib"] || !settings["enrichment"]["google_books"] {
		t.Errorf("default settings = %v", settings)
	}

	// Disable livelib
	resp = postJSON(t, client, ts.URL+"/admin/settings", map[string]any{
		"enrichment": map[string]bool{"livelib": false},
	})
	json.NewDecoder(resp.Body).Decode(&settings)
	resp.Body.Close()
	if settings["enrichment"]["livelib"] || !settings["enrichment"]["open_library"] {
		t.Errorf("after save = %v", settings)
	}

	// Rating a book
	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+bookID+"/rating", map[string]int{"rating": 5})
	var rated map[string]any
	json.NewDecoder(resp.Body).Decode(&rated)
	resp.Body.Close()
	if rated["userRating"].(float64) != 5 {
		t.Fatalf("rate response = %v", rated)
	}
	pr := rated["polkaRating"].(map[string]any)
	if pr["rating"].(float64) != 5 || pr["count"].(float64) != 1 {
		t.Errorf("polkaRating = %v", pr)
	}

	// The rating is visible to everyone: a second user sees the average and casts their own
	postJSON(t, client, ts.URL+"/admin/users", map[string]any{
		"login": "reader", "password": "readpass1",
	}).Body.Close()
	reader := newClientLoggedIn(t, ts.URL, "reader", "readpass1")

	var form map[string]any
	resp, _ = reader.Get(ts.URL + "/main/getBooks/getBookForm?selectedItemID=" + bookID)
	json.NewDecoder(resp.Body).Decode(&form)
	resp.Body.Close()
	if form["polkaRating"].(map[string]any)["rating"].(float64) != 5 {
		t.Errorf("global rating not visible: %v", form["polkaRating"])
	}
	if form["userRating"].(float64) != 0 {
		t.Errorf("reader own rating = %v", form["userRating"])
	}

	resp = postJSON(t, reader, ts.URL+"/api/v1/books/"+bookID+"/rating", map[string]int{"rating": 3})
	json.NewDecoder(resp.Body).Decode(&rated)
	resp.Body.Close()
	pr = rated["polkaRating"].(map[string]any)
	if pr["rating"].(float64) != 4 || pr["count"].(float64) != 2 {
		t.Errorf("avg after two votes = %v", pr)
	}

	// Removing a rating
	resp = postJSON(t, reader, ts.URL+"/api/v1/books/"+bookID+"/rating", map[string]int{"rating": 0})
	json.NewDecoder(resp.Body).Decode(&rated)
	resp.Body.Close()
	if rated["polkaRating"].(map[string]any)["count"].(float64) != 1 {
		t.Errorf("after unrate = %v", rated)
	}

	// Settings are unavailable to non-admins
	resp, _ = reader.Get(ts.URL + "/admin/settings")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("settings as reader -> %d", resp.StatusCode)
	}
}

func newClientLoggedIn(t *testing.T, baseURL, login, password string) *http.Client {
	t.Helper()
	jar := newJar(t)
	c := &http.Client{Jar: jar}
	resp := postJSON(t, c, baseURL+"/auth/login", map[string]string{"login": login, "password": password})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login %s -> %d", login, resp.StatusCode)
	}
	return c
}
