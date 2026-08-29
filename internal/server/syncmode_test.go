package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
	"github.com/vestigiumincaligne/polka/internal/syncer"
)

// upstream is a real Polka server (one book, one user) behind a gate
// that can be switched off to simulate losing the connection.
type upstream struct {
	ts     *httptest.Server
	users  *auth.Service
	owner  *auth.User
	bookID int64
	down   atomic.Bool
}

const upstreamLogin, upstreamPassword = "owner", "secret123"

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	dir := t.TempDir()
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(libDir, 0o755)
	zf, _ := os.Create(filepath.Join(libDir, "arch-001.zip"))
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("100.fb2")
	w.Write([]byte(testFB2))
	zw.Close()
	zf.Close()

	st, err := store.Open(filepath.Join(dir, "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, _ := st.NewImport(ctx)
	session.Add(&store.BookInput{
		Title: "Война и мир", Authors: []store.AuthorName{{Last: "Толстой", First: "Лев"}},
		Series: "Классика", SeriesNum: 1,
		Folder: "arch-001.zip", File: "100", Ext: "fb2", Size: int64(len(testFB2)), Lang: "ru", Added: "2024-01-01", Rate: 5,
	})
	session.Finish()
	var bookID int64
	st.DB().QueryRow(`SELECT id FROM books`).Scan(&bookID)

	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })
	owner, _ := users.CreateUser(ctx, upstreamLogin, upstreamPassword, "", auth.RoleAdmin)

	cfg := &config.Config{LibraryDir: libDir, DataDir: dir, Auth: "required"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html/>")}}
	srv := New(cfg, log, st, library.New(libDir), users, nil, webFS)

	u := &upstream{users: users, owner: owner, bookID: bookID}
	u.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u.down.Load() {
			http.Error(w, "gateway down", http.StatusServiceUnavailable)
			return
		}
		srv.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(u.ts.Close)
	return u
}

// state fetches the owner's sync state straight from the upstream.
func (u *upstream) state(t *testing.T) auth.SyncState {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u.ts.URL+"/api/v1/sync/state", nil)
	req.SetBasicAuth(upstreamLogin, upstreamPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st auth.SyncState
	json.NewDecoder(resp.Body).Decode(&st)
	return st
}

// newDesktopClient builds a desktop-mode server; with up != nil it runs
// in sync mode against that upstream.
func newDesktopClient(t *testing.T, up *upstream) (*httptest.Server, *auth.Service, string) {
	t.Helper()
	dir := t.TempDir()
	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })
	st, err := store.Open(filepath.Join(dir, "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var sy *syncer.Syncer
	if up != nil {
		sy, err = syncer.New(dir, syncer.Config{Server: up.ts.URL, Login: upstreamLogin, Password: upstreamPassword}, users, log)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sy.Close() })
	}
	cfg := &config.Config{DataDir: dir, Auth: "desktop"}
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html/>")}}
	srv := New(cfg, log, st, nil, users, sy, webFS)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, users, dir
}

func statusOf(t *testing.T, method, url string, body string) (int, []byte, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("X-Polka-Lang", "ru")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, resp.Header
}

func jsonOf(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("not json: %s", data)
	}
	return out
}

func TestSyncModeOfflineAndOnline(t *testing.T) {
	up := newUpstream(t)
	client, users, _ := newDesktopClient(t, up)
	ctx := context.Background()
	id := up.bookID
	idStr := itoa64(id)

	// --- Fresh start: the syncer has not checked connectivity yet → offline. ---
	code, data, _ := statusOf(t, "GET", client.URL+"/api/v1/sync/info", "")
	info := jsonOf(t, data)
	if code != 200 || info["enabled"] != true || info["online"] != false || info["lastSync"] != nil {
		t.Errorf("initial sync/info: %d %v", code, info)
	}
	if _, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getHomeShelves", ""); len(jsonOf(t, data)["shelves"].([]any)) != 0 {
		t.Errorf("offline home with empty cache must have no shelves: %s", data)
	}
	if _, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getConfig", ""); jsonOf(t, data)["collectionName"] != "Офлайн-библиотека" {
		t.Errorf("offline config: %s", data)
	}
	for _, p := range []string{
		"/main/getBooks/getShelfBooks?shelfId=newest",
		"/main/getBooks/getSearchAuthors?search=abc",
		"/Images/zip/" + idStr,
		"/admin/settings",
	} {
		if code, _, _ := statusOf(t, "GET", client.URL+p, ""); code != http.StatusServiceUnavailable {
			t.Errorf("offline %s -> %d, want 503", p, code)
		}
	}
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/offline/"+idStr, ""); code != http.StatusServiceUnavailable {
		t.Errorf("offline download -> %d, want 503", code)
	}
	if _, data, _ = statusOf(t, "GET", client.URL+"/api/v1/offline/"+idStr, ""); jsonOf(t, data)["offline"] != false {
		t.Errorf("offline status: %s", data)
	}

	// --- Manual sync brings the client online. ---
	code, data, _ = statusOf(t, "POST", client.URL+"/api/v1/sync/now", "")
	info = jsonOf(t, data)
	if code != 200 || info["online"] != true || info["lastSync"] == nil {
		t.Fatalf("sync/now: %d %s", code, data)
	}

	// Catalog is proxied to the upstream with the stored credentials.
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getHomeShelves", "")
	if !strings.Contains(string(data), `"newest"`) || !strings.Contains(string(data), "Война и мир") {
		t.Errorf("online home shelves must come from the server: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getSearchTitles?search=Война", "")
	if n := len(jsonOf(t, data)["titlesList"].([]any)); n != 1 {
		t.Errorf("proxied search: %d results", n)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getBookForm?selectedItemID="+idStr, "")
	if form := jsonOf(t, data)["bookForm"].(map[string]any); form["Title"] != "Война и мир" {
		t.Errorf("proxied book form: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getConfig", "")
	if jsonOf(t, data)["numberOfBooks"].(float64) != 1 {
		t.Errorf("proxied config: %s", data)
	}
	if code, _, _ := statusOf(t, "GET", client.URL+"/Images/fb2/"+idStr, ""); code != 200 {
		t.Errorf("proxied download -> %d", code)
	}

	// --- Two-way sync of user data. ---
	// Local change → pushed on the next sync.
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/read/"+idStr+"/progress", `{"chapter":2,"position":0.5,"progress":0.4}`); code != 200 {
		t.Fatalf("save progress -> %d", code)
	}
	// Server-side change → pulled.
	up.users.RateBook(ctx, up.owner.ID, id, 4)
	statusOf(t, "POST", client.URL+"/api/v1/sync/now", "")

	if st := up.state(t); len(st.Progress) != 1 || st.Progress[0].BookID != id || st.Progress[0].Chapter != 2 {
		t.Errorf("progress not pushed to the server: %+v", st.Progress)
	}
	owner, _ := users.EnsureLogin(ctx, "desktop", "Владелец", auth.RoleAdmin)
	if r := users.UserRating(ctx, owner.ID, id); r != 4 {
		t.Errorf("rating not pulled from the server: %d", r)
	}

	// --- Download for offline use. ---
	code, data, _ = statusOf(t, "POST", client.URL+"/api/v1/offline/"+idStr, "")
	if code != 200 || jsonOf(t, data)["title"] != "Война и мир" {
		t.Fatalf("make offline: %d %s", code, data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/offline", "")
	if books := jsonOf(t, data)["books"].([]any); len(books) != 1 || books[0].(map[string]any)["ext"] != "fb2" {
		t.Errorf("offline list: %s", data)
	}

	// Lists resolve cards for offline books from the cache.
	_, data, _ = statusOf(t, "POST", client.URL+"/api/v1/lists", `{"name":"Отпуск"}`)
	listID := itoa64(int64(jsonOf(t, data)["list"].(map[string]any)["id"].(float64)))
	statusOf(t, "POST", client.URL+"/api/v1/lists/"+listID+"/books", `{"bookId":`+idStr+`}`)

	// --- The server goes away. ---
	up.down.Store(true)
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/sync/now", ""); code != http.StatusServiceUnavailable {
		t.Errorf("sync/now while down -> %d", code)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/sync/info", "")
	if jsonOf(t, data)["online"] != false {
		t.Errorf("must be offline after a failed check: %s", data)
	}

	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getHomeShelves", "")
	shelves := jsonOf(t, data)["shelves"].([]any)
	if len(shelves) != 1 || shelves[0].(map[string]any)["id"] != "offline" {
		t.Errorf("offline home must show the offline shelf: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getConfig", "")
	if jsonOf(t, data)["numberOfBooks"].(float64) != 1 {
		t.Errorf("offline config counts cached books: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getBookForm?selectedItemID="+idStr, "")
	form := jsonOf(t, data)["bookForm"].(map[string]any)
	if form["Title"] != "Война и мир" || form["Ext"] != ".fb2" || jsonOf(t, data)["userRating"].(float64) != 4 {
		t.Errorf("cached book form: %s", data)
	}
	if code, _, _ := statusOf(t, "GET", client.URL+"/main/getBooks/getBookForm?selectedItemID=99999", ""); code != 404 {
		t.Errorf("uncached book form -> %d, want 404", code)
	}
	for q, want := range map[string]int{"мир": 1, "толстой": 1, "zzz": 0, "": 1} {
		_, data, _ = statusOf(t, "GET", client.URL+"/main/getBooks/getSearchTitles?search="+q, "")
		if n := len(jsonOf(t, data)["titlesList"].([]any)); n != want {
			t.Errorf("offline search %q: %d, want %d", q, n, want)
		}
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/lists/"+listID+"/books", "")
	if tl := jsonOf(t, data)["titlesList"].([]any); len(tl) != 1 || tl[0].(map[string]any)["Title"] != "Война и мир" {
		t.Errorf("list books offline: %s", data)
	}

	// Files and covers from the cache.
	code, data, hdr := statusOf(t, "GET", client.URL+"/Images/fb2/"+idStr, "")
	if code != 200 || string(data) != testFB2 || !strings.HasPrefix(hdr.Get("Content-Disposition"), "attachment") {
		t.Errorf("cached download: %d %q %q", code, hdr.Get("Content-Disposition"), data[:20])
	}
	if _, _, hdr = statusOf(t, "GET", client.URL+"/Images/fb2/"+idStr+"?inline=1", ""); !strings.HasPrefix(hdr.Get("Content-Disposition"), "inline") {
		t.Errorf("inline download: %q", hdr.Get("Content-Disposition"))
	}
	if code, _, hdr = statusOf(t, "GET", client.URL+"/Images/covers/"+idStr, ""); code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "image/") {
		t.Errorf("cached cover: %d %q", code, hdr.Get("Content-Type"))
	}
	if code, _, _ = statusOf(t, "GET", client.URL+"/Images/covers/99999", ""); code != 404 {
		t.Errorf("uncached cover offline -> %d", code)
	}

	// Reading from the cache.
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/read/"+idStr, "")
	meta := jsonOf(t, data)
	if meta["format"] != "fb2" || len(meta["chapters"].([]any)) == 0 {
		t.Errorf("offline read meta: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/read/"+idStr+"/chapter/0", "")
	if !strings.Contains(jsonOf(t, data)["html"].(string), "Текст") {
		t.Errorf("offline chapter: %s", data)
	}
	if code, _, _ = statusOf(t, "GET", client.URL+"/api/v1/read/"+idStr+"/chapter/99", ""); code != 404 {
		t.Errorf("chapter out of range -> %d", code)
	}
	if code, _, _ = statusOf(t, "GET", client.URL+"/api/v1/read/"+idStr+"/img/c.png", ""); code != 200 {
		t.Errorf("offline image -> %d", code)
	}
	if code, _, _ = statusOf(t, "GET", client.URL+"/api/v1/read/99999", ""); code != http.StatusServiceUnavailable {
		t.Errorf("uncached read offline -> %d", code)
	}
	if code, _, _ = statusOf(t, "GET", client.URL+"/api/v1/read/99999/chapter/0", ""); code != http.StatusServiceUnavailable {
		t.Errorf("uncached chapter offline -> %d", code)
	}

	// Writes while offline land locally and are pushed once the server is back.
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/read/"+idStr+"/progress", `{"chapter":7,"position":0.1,"progress":0.9}`); code != 200 {
		t.Fatalf("offline progress -> %d", code)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/read/"+idStr+"/progress", "")
	if jsonOf(t, data)["chapter"].(float64) != 7 {
		t.Errorf("offline progress not stored locally: %s", data)
	}
	up.down.Store(false)
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/sync/now", ""); code != 200 {
		t.Fatalf("sync after reconnect -> %d", code)
	}
	if st := up.state(t); len(st.Progress) != 1 || st.Progress[0].Chapter != 7 {
		t.Errorf("offline change not pushed after reconnect: %+v", st.Progress)
	}

	// --- Removing from the cache. ---
	if _, data, _ = statusOf(t, "POST", client.URL+"/api/v1/offline/"+idStr+"/delete", ""); jsonOf(t, data)["offline"] != false {
		t.Errorf("remove offline: %s", data)
	}
	if _, data, _ = statusOf(t, "GET", client.URL+"/api/v1/offline/"+idStr, ""); jsonOf(t, data)["offline"] != false {
		t.Errorf("status after remove: %s", data)
	}
	if code, _, _ := statusOf(t, "POST", client.URL+"/api/v1/offline/"+idStr+"/delete", ""); code != 200 {
		t.Errorf("removing twice must be fine, got %d", code)
	}
	// Online → the download is proxied; offline → 503.
	if code, _, _ := statusOf(t, "GET", client.URL+"/Images/fb2/"+idStr, ""); code != 200 {
		t.Errorf("proxied download after remove -> %d", code)
	}
	up.down.Store(true)
	statusOf(t, "POST", client.URL+"/api/v1/sync/now", "") // flips the state to offline
	if code, _, _ := statusOf(t, "GET", client.URL+"/Images/fb2/"+idStr, ""); code != http.StatusServiceUnavailable {
		t.Errorf("download offline without cache -> %d", code)
	}
	if code, _, _ := statusOf(t, "GET", client.URL+"/Images/covers/"+idStr, ""); code != 404 {
		t.Errorf("cover offline without cache -> %d", code)
	}
	// Bad ids.
	for _, p := range []string{"/api/v1/offline/abc", "/api/v1/offline/0", "/api/v1/read/abc"} {
		if code, _, _ := statusOf(t, "GET", client.URL+p, ""); code != 404 {
			t.Errorf("%s -> %d, want 404", p, code)
		}
	}
}

func TestDesktopSyncConfig(t *testing.T) {
	up := newUpstream(t)
	client, _, dir := newDesktopClient(t, nil) // desktop without sync

	_, data, _ := statusOf(t, "GET", client.URL+"/api/v1/sync/info", "")
	if jsonOf(t, data)["enabled"] != false {
		t.Errorf("sync/info without sync: %s", data)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/sync/config", "")
	if cfg := jsonOf(t, data); cfg["configured"] != false || cfg["active"] != false {
		t.Errorf("initial config: %s", data)
	}

	cases := []struct {
		body string
		want int
	}{
		{`not json`, 400},
		{`{"server":"` + up.ts.URL + `","login":"","password":"x"}`, 400},
		{`{"server":"` + up.ts.URL + `","login":"owner","password":"wrong"}`, 401},
		{`{"server":"http://127.0.0.1:1","login":"owner","password":"x"}`, 502},
		{`{"server":"` + up.ts.URL + `","login":"owner","password":"` + upstreamPassword + `"}`, 200},
	}
	for _, c := range cases {
		if code, data, _ := statusOf(t, "POST", client.URL+"/api/v1/sync/config", c.body); code != c.want {
			t.Errorf("save %s -> %d (%s), want %d", c.body, code, bytes.TrimSpace(data), c.want)
		}
	}
	if _, err := os.Stat(syncer.ConfigPath(dir)); err != nil {
		t.Fatalf("sync.json not written: %v", err)
	}
	_, data, _ = statusOf(t, "GET", client.URL+"/api/v1/sync/config", "")
	cfg := jsonOf(t, data)
	if cfg["configured"] != true || cfg["server"] != up.ts.URL || cfg["login"] != "owner" || cfg["password"] != nil {
		t.Errorf("saved config: %s", data)
	}
	if code, data, _ := statusOf(t, "POST", client.URL+"/api/v1/sync/disconnect", ""); code != 200 || jsonOf(t, data)["restartRequired"] != true {
		t.Errorf("disconnect: %d %s", code, data)
	}
	if _, err := os.Stat(syncer.ConfigPath(dir)); !os.IsNotExist(err) {
		t.Error("sync.json must be removed")
	}
	if _, data, _ = statusOf(t, "GET", client.URL+"/api/v1/sync/config", ""); jsonOf(t, data)["configured"] != false {
		t.Errorf("config after disconnect: %s", data)
	}
	// Desktop mode has no user management: the route is not registered,
	// so the request falls through to the SPA shell instead of the API.
	if _, _, hdr := statusOf(t, "GET", client.URL+"/admin/users", ""); !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") {
		t.Errorf("/admin/users in desktop mode must not be an API route, got %q", hdr.Get("Content-Type"))
	}
}
