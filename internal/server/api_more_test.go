package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

func TestCatalogAndSeriesEndpoints(t *testing.T) {
	f := newFullServer(t, "required")
	c := f.reader
	id := itoa64(f.bookID)

	shelves := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getCatalogShelves?count=5&limit=1")["shelves"].([]any)
	if len(shelves) != 1 {
		t.Fatalf("catalog shelves: %v", shelves)
	}
	sh := shelves[0].(map[string]any)
	if sh["id"] != "genre_prose_classic" || sh["title"] == "" || strings.HasPrefix(sh["title"].(string), "genre_") || len(sh["books"].([]any)) != 1 || sh["hasMore"] != true {
		t.Errorf("genre shelf must be localized and paged: %v", sh)
	}
	page := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getShelfBooks?shelfId=genre_prose_classic&limit=1&offset=1")
	if page["title"] != sh["title"] || len(page["titlesList"].([]any)) != 1 || page["hasMore"] != false || page["nextOffset"].(float64) != 2 {
		t.Errorf("genre shelf page 2: %v", page)
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getShelfBooks?shelfId=genre_nope", "") != http.StatusNotFound {
		t.Error("unknown genre shelf must be 404")
	}

	byIDs := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getBooksByIds?ids="+itoa64(f.bookID2)+",%20"+id+",abc,99999")["titlesList"].([]any)
	if len(byIDs) != 2 || byIDs[0].(map[string]any)["Title"] != "Анна Каренина" {
		t.Errorf("getBooksByIds keeps order and skips unknown: %v", byIDs)
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getBooksByIds?ids=", "") != http.StatusBadRequest {
		t.Error("empty ids -> 400")
	}
	many := strings.Repeat("1,", 501)
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getBooksByIds?ids="+many, "") != http.StatusBadRequest {
		t.Error("more than 500 ids -> 400")
	}

	series := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getSearchSeriesBooks?selectedItemID="+itoa64(f.seriesID))
	if series["title"] != "Классика" || len(series["titlesList"].([]any)) != 2 {
		t.Errorf("series books: %v", series)
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getSearchSeriesBooks", "") != http.StatusBadRequest {
		t.Error("series without id -> 400")
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getSearchSeriesBooks?selectedItemID=99999", "") != http.StatusNotFound {
		t.Error("unknown series -> 404")
	}

	similar := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getSimilarBooks?bookId="+id+"&title=Война&author=Толстой")
	if _, ok := similar["similar"]; !ok {
		t.Errorf("similar books response: %v", similar)
	}
	if ext, _ := similar["external"].([]any); len(ext) != 0 {
		t.Errorf("external sources are off, got: %v", ext)
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getSimilarBooks", "") != http.StatusBadRequest {
		t.Error("similar without bookId -> 400")
	}

	// External enrichment: with every source switched off (see newFullServer)
	// it answers without touching the network.
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getExternalEnrichment?bookId="+id+"&title=Война+и+мир", "") != http.StatusOK {
		t.Error("enrichment with sources off must still answer")
	}
	if code(t, c, "GET", f.ts.URL+"/main/getBooks/getExternalEnrichment?bookId="+id, "") != http.StatusBadRequest {
		t.Error("enrichment without title -> 400")
	}

	// Reader images come straight out of the FB2.
	resp := do(t, c, "GET", f.ts.URL+"/api/v1/read/"+id+"/img/c.png", "")
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || len(data) == 0 {
		t.Errorf("read image: %d %q %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(data))
	}
	if code(t, c, "GET", f.ts.URL+"/api/v1/read/"+id+"/img/nope.png", "") != http.StatusNotFound {
		t.Error("unknown image -> 404")
	}

	for ext, want := range map[string]string{"fb2": "application/x-fictionbook+xml; charset=utf-8", "EPUB": "application/epub+zip",
		"pdf": "application/pdf", "djvu": "image/vnd.djvu", "txt": "text/plain; charset=utf-8", "mobi": "application/octet-stream"} {
		if got := contentTypeFor(ext); got != want {
			t.Errorf("contentTypeFor(%q) = %q", ext, got)
		}
	}
}

func TestReadingShelfAndOpdsReading(t *testing.T) {
	f := newFullServer(t, "required")
	c := f.reader
	id := itoa64(f.bookID)

	home := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getHomeShelves")
	for _, sh := range home["shelves"].([]any) {
		if sh.(map[string]any)["id"] == "reading" {
			t.Fatal("no reading shelf before any progress")
		}
	}
	postJSON(t, c, f.ts.URL+"/api/v1/read/"+id+"/progress", map[string]any{"chapter": 0, "position": 0.5, "progress": 0.25}).Body.Close()
	home = getJSONWith(t, c, f.ts.URL+"/main/getBooks/getHomeShelves")
	found := false
	for _, sh := range home["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "reading" {
			found = true
			books := m["books"].([]any)
			if len(books) != 1 || books[0].(map[string]any)["ReadingProgress"].(float64) != 0.25 {
				t.Errorf("reading shelf: %v", books)
			}
		}
	}
	if !found {
		t.Error("reading shelf missing after progress")
	}

	// OPDS "reading now" for the same user via Basic auth.
	req, _ := http.NewRequest("GET", f.ts.URL+"/opds/reading", nil)
	req.SetBasicAuth("reader", "reader123")
	resp, _ := http.DefaultClient.Do(req)
	feed, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(feed), "Война и мир") {
		t.Errorf("opds reading: %d %s", resp.StatusCode, feed)
	}
	// Series browsing in OPDS.
	for _, p := range []string{"/opds/series/К", "/opds/series/id/" + itoa64(f.seriesID)} {
		resp := do(t, c, "GET", f.ts.URL+p, "")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), "Классика") {
			t.Errorf("%s: %d %.200s", p, resp.StatusCode, body)
		}
	}
	for _, p := range []string{"/opds/series/КК", "/opds/series/id/99999", "/opds/series/id/x", "/opds/authors/12"} {
		if got := code(t, c, "GET", f.ts.URL+p, ""); got != http.StatusNotFound {
			t.Errorf("%s -> %d, want 404", p, got)
		}
	}
	resp = do(t, c, "GET", f.ts.URL+"/opds/authors", "")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "/opds/authors/") {
		t.Errorf("authors letters feed: %.200s", body)
	}
}

func TestListRenameAndRemove(t *testing.T) {
	f := newFullServer(t, "required")
	c := f.reader
	id := itoa64(f.bookID)

	resp := postJSON(t, c, f.ts.URL+"/api/v1/lists", map[string]string{"name": "Отпуск"})
	var created struct {
		List struct {
			ID float64 `json:"id"`
		} `json:"list"`
	}
	decode(t, resp, &created)
	listID := itoa64(int64(created.List.ID))
	postJSON(t, c, f.ts.URL+"/api/v1/lists", map[string]string{"name": "Работа"}).Body.Close()

	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+listID, `{"name":"Лето"}`); got != 200 {
		t.Errorf("rename -> %d", got)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+listID, `{"name":"Работа"}`); got != http.StatusConflict {
		t.Errorf("rename to a taken name -> %d, want 409", got)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+listID, `oops`); got != http.StatusBadRequest {
		t.Errorf("rename bad body -> %d", got)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/99999", `{"name":"x"}`); got != http.StatusNotFound {
		t.Errorf("rename unknown -> %d", got)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/abc", `{"name":"x"}`); got != http.StatusNotFound {
		t.Errorf("rename bad id -> %d", got)
	}
	// The wishlist is builtin (created on first use): no rename, no delete.
	postJSON(t, c, f.ts.URL+"/api/v1/books/"+id+"/wishlist", map[string]any{"add": true}).Body.Close()
	wl := getJSONWith(t, c, f.ts.URL+"/api/v1/lists")["lists"].([]any)
	var wishID string
	for _, l := range wl {
		if l.(map[string]any)["builtin"] == auth.BuiltinWishlist {
			wishID = itoa64(int64(l.(map[string]any)["id"].(float64)))
		}
	}
	if wishID == "" {
		t.Fatal("wishlist missing")
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+wishID, `{"name":"x"}`); got != http.StatusConflict {
		t.Errorf("rename builtin -> %d, want 409", got)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+wishID+"/delete", ""); got != http.StatusConflict {
		t.Errorf("delete builtin -> %d, want 409", got)
	}

	postJSON(t, c, f.ts.URL+"/api/v1/lists/"+listID+"/books", map[string]any{"bookId": f.bookID}).Body.Close()
	if n := len(getJSONWith(t, c, f.ts.URL+"/api/v1/lists/"+listID+"/books")["titlesList"].([]any)); n != 1 {
		t.Fatalf("books in list: %d", n)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+listID+"/books/remove", `{"bookId":`+id+`}`); got != 200 {
		t.Errorf("remove -> %d", got)
	}
	if n := len(getJSONWith(t, c, f.ts.URL+"/api/v1/lists/"+listID+"/books")["titlesList"].([]any)); n != 0 {
		t.Errorf("books after remove: %d", n)
	}
	if got := code(t, c, "POST", f.ts.URL+"/api/v1/lists/"+listID+"/books/remove", `{"bookId":0}`); got != http.StatusBadRequest {
		t.Errorf("remove without bookId -> %d", got)
	}
	// Another user's list is invisible.
	if got := code(t, f.admin, "POST", f.ts.URL+"/api/v1/lists/"+listID+"/books", `{"bookId":`+id+`}`); got != http.StatusNotFound {
		t.Errorf("foreign list -> %d, want 404", got)
	}
}

func TestUserUpdate(t *testing.T) {
	f := newFullServer(t, "required")
	ctx := context.Background()
	users := getJSONWith(t, f.admin, f.ts.URL+"/admin/users")["users"].([]any)
	var readerID, adminID string
	for _, u := range users {
		m := u.(map[string]any)
		switch m["login"] {
		case "reader":
			readerID = itoa64(int64(m["id"].(float64)))
		case "admin":
			adminID = itoa64(int64(m["id"].(float64)))
		}
	}

	resp := postJSON(t, f.admin, f.ts.URL+"/admin/users/"+readerID, map[string]any{"displayName": "Новое имя", "role": auth.RoleAdmin})
	var upd struct {
		User map[string]any `json:"user"`
	}
	decode(t, resp, &upd)
	if upd.User["displayName"] != "Новое имя" || upd.User["role"] != auth.RoleAdmin {
		t.Errorf("update: %v", upd.User)
	}
	// Now the reader is an admin too and may be demoted back.
	postJSON(t, f.admin, f.ts.URL+"/admin/users/"+readerID, map[string]any{"role": auth.RoleUser}).Body.Close()
	// The last administrator cannot be demoted or disabled.
	if got := code(t, f.admin, "POST", f.ts.URL+"/admin/users/"+adminID, `{"role":"user"}`); got != http.StatusConflict {
		t.Errorf("demote last admin -> %d, want 409", got)
	}
	if got := code(t, f.admin, "POST", f.ts.URL+"/admin/users/"+adminID+"/delete", ""); got != http.StatusConflict {
		t.Errorf("delete last admin -> %d, want 409", got)
	}
	// Password change takes effect; disabled users cannot log in.
	postJSON(t, f.admin, f.ts.URL+"/admin/users/"+readerID, map[string]any{"password": "newpass123"}).Body.Close()
	if resp := postJSON(t, &http.Client{}, f.ts.URL+"/auth/login", map[string]string{"login": "reader", "password": "newpass123"}); resp.StatusCode != 200 {
		t.Errorf("login with new password -> %d", resp.StatusCode)
	}
	postJSON(t, f.admin, f.ts.URL+"/admin/users/"+readerID, map[string]any{"disabled": true}).Body.Close()
	if resp := postJSON(t, &http.Client{}, f.ts.URL+"/auth/login", map[string]string{"login": "reader", "password": "newpass123"}); resp.StatusCode == 200 {
		t.Error("disabled user must not log in")
	}
	for path, body := range map[string]string{"/admin/users/abc": `{}`, "/admin/users/99999": `{"role":"user"}`, "/admin/users/" + readerID: `nope`} {
		got := code(t, f.admin, "POST", f.ts.URL+path, body)
		if got != http.StatusNotFound && got != http.StatusBadRequest {
			t.Errorf("%s with %q -> %d", path, body, got)
		}
	}
	if n, _ := f.users.UsersCount(ctx); n != 2 {
		t.Errorf("users: %d", n)
	}
}

func TestSmtpTest(t *testing.T) {
	f := newFullServer(t, "required")
	res := getJSONWith(t, f.admin, f.ts.URL+"/admin/settings")["smtp"].(map[string]any)
	if res["enabled"] != false || res["hasPassword"] != false {
		t.Errorf("default smtp settings: %v", res)
	}
	resp := postJSON(t, f.admin, f.ts.URL+"/admin/smtp/test", nil)
	var out map[string]any
	decode(t, resp, &out)
	if out["ok"] != false || out["error"] == "" {
		t.Errorf("smtp test without config: %v", out)
	}
	host, port, _ := fakeSMTP(t)
	postJSON(t, f.admin, f.ts.URL+"/admin/settings", map[string]any{
		"smtp": map[string]any{"enabled": true, "host": host, "port": port, "from": "lib@polka.test", "security": "none", "password": "pw"},
	}).Body.Close()
	res = getJSONWith(t, f.admin, f.ts.URL+"/admin/settings")["smtp"].(map[string]any)
	if res["enabled"] != true || res["host"] != host || res["hasPassword"] != true || res["password"] != nil {
		t.Errorf("saved smtp settings must not leak the password: %v", res)
	}
	resp = postJSON(t, f.admin, f.ts.URL+"/admin/smtp/test", nil)
	decode(t, resp, &out)
	if out["ok"] != true {
		t.Errorf("smtp test against the fake server: %v", out)
	}
}

// decode reads a JSON response body into v and closes it.
func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", resp.Request.URL, err)
	}
}
