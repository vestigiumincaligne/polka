package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReadingLists(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"a.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	bookID := up["results"][0].BookID

	// The "Want to read" toggle creates the builtin list
	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(bookID)+"/wishlist", map[string]any{"add": true})
	var wt map[string]any
	json.NewDecoder(resp.Body).Decode(&wt)
	resp.Body.Close()
	if wt["inWishlist"] != true {
		t.Fatalf("wishlist toggle = %v", wt)
	}

	// The "Want to read" shelf on the home page
	var shelves map[string][]map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	found := false
	for _, sh := range shelves["shelves"] {
		if sh["id"] == "wishlist" {
			found = true
			if n := len(sh["books"].([]any)); n != 1 {
				t.Errorf("wishlist shelf books = %d", n)
			}
		}
	}
	if !found {
		t.Error("wishlist shelf missing on home")
	}

	// Custom list: create, add, list books, rename
	resp = postJSON(t, client, ts.URL+"/api/v1/lists", map[string]string{"name": "Отпуск"})
	var created map[string]map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	listID := int64(created["list"]["id"].(float64))

	resp = postJSON(t, client, ts.URL+"/api/v1/lists/"+itoa64(listID)+"/books", map[string]any{"bookId": bookID})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("add to list -> %d", resp.StatusCode)
	}

	var lb map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/lists/" + itoa64(listID) + "/books")
	json.NewDecoder(resp.Body).Decode(&lb)
	resp.Body.Close()
	if len(lb["titlesList"].([]any)) != 1 {
		t.Errorf("list books = %v", lb)
	}

	// The book card knows which lists the book is in
	var form map[string]any
	resp, _ = client.Get(ts.URL + "/main/getBooks/getBookForm?selectedItemID=" + itoa64(bookID))
	json.NewDecoder(resp.Body).Decode(&form)
	resp.Body.Close()
	if ids := form["bookListIds"].([]any); len(ids) != 2 {
		t.Errorf("bookListIds = %v", ids)
	}

	// Duplicate list name
	resp = postJSON(t, client, ts.URL+"/api/v1/lists", map[string]string{"name": "Отпуск"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate list name -> %d", resp.StatusCode)
	}

	// The builtin list cannot be deleted, a custom one can
	var lists map[string][]map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/lists")
	json.NewDecoder(resp.Body).Decode(&lists)
	resp.Body.Close()
	if len(lists["lists"]) != 2 {
		t.Fatalf("lists count = %d", len(lists["lists"]))
	}
	for _, l := range lists["lists"] {
		id := int64(l["id"].(float64))
		resp = postJSON(t, client, ts.URL+"/api/v1/lists/"+itoa64(id)+"/delete", nil)
		resp.Body.Close()
		if l["builtin"] == "wishlist" && resp.StatusCode != http.StatusConflict {
			t.Errorf("delete builtin -> %d", resp.StatusCode)
		}
		if l["builtin"] == "" && resp.StatusCode != 200 {
			t.Errorf("delete custom -> %d", resp.StatusCode)
		}
	}

	// Removing from "Want to read"
	resp = postJSON(t, client, ts.URL+"/api/v1/books/"+itoa64(bookID)+"/wishlist", map[string]any{"add": false})
	resp.Body.Close()
	resp, _ = client.Get(ts.URL + "/main/getBooks/getHomeShelves")
	json.NewDecoder(resp.Body).Decode(&shelves)
	resp.Body.Close()
	for _, sh := range shelves["shelves"] {
		if sh["id"] == "wishlist" {
			t.Error("empty wishlist shelf must disappear")
		}
	}

	// Another user's list is inaccessible
	postJSON(t, client, ts.URL+"/admin/users", map[string]any{"login": "reader", "password": "readpass1"}).Body.Close()
	reader := newClientLoggedIn(t, ts.URL, "reader", "readpass1")
	resp = postJSON(t, reader, ts.URL+"/api/v1/lists", map[string]string{"name": "Своё"})
	var rlist map[string]map[string]any
	json.NewDecoder(resp.Body).Decode(&rlist)
	resp.Body.Close()
	foreignID := int64(rlist["list"]["id"].(float64))
	resp = postJSON(t, client, ts.URL+"/api/v1/lists/"+itoa64(foreignID)+"/books", map[string]any{"bookId": bookID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("foreign list access -> %d, want 404", resp.StatusCode)
	}
}
