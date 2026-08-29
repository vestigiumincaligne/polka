package server

import (
	"archive/zip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// fullServer: a library with two books in a series, an administrator and
// a plain reader, both logged in.
type fullServer struct {
	ts            *httptest.Server
	admin, reader *http.Client
	users         *auth.Service
	st            *store.Store
	bookID        int64 // «Война и мир»
	bookID2       int64 // «Анна Каренина»
	seriesID      int64
	authorID      int64
	dir           string
}

func newFullServer(t *testing.T, mode string) *fullServer {
	t.Helper()
	dir := t.TempDir()
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(libDir, 0o755)
	zf, _ := os.Create(filepath.Join(libDir, "arch-001.zip"))
	zw := zip.NewWriter(zf)
	for _, name := range []string{"100.fb2", "101.fb2"} {
		w, _ := zw.Create(name)
		w.Write([]byte(testFB2))
	}
	zw.Close()
	zf.Close()

	st, err := store.Open(filepath.Join(dir, "polka.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, _ := st.NewImport(ctx)
	for i, title := range []string{"Война и мир", "Анна Каренина"} {
		session.Add(&store.BookInput{
			Title: title, Authors: []store.AuthorName{{Last: "Толстой", First: "Лев"}},
			Genres: []string{"prose_classic"}, Series: "Классика", SeriesNum: i + 1,
			Folder: "arch-001.zip", File: itoa(100 + i), Ext: "fb2", Size: int64(len(testFB2)),
			Lang: "ru", Added: "2024-01-0" + itoa(i+1), Rate: 5,
		})
	}
	session.Finish()
	f := &fullServer{st: st, dir: dir}
	st.DB().QueryRow(`SELECT id FROM books WHERE title = 'Война и мир'`).Scan(&f.bookID)
	st.DB().QueryRow(`SELECT id FROM books WHERE title = 'Анна Каренина'`).Scan(&f.bookID2)
	st.DB().QueryRow(`SELECT id FROM series`).Scan(&f.seriesID)
	st.DB().QueryRow(`SELECT id FROM authors`).Scan(&f.authorID)

	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })
	f.users = users
	users.CreateUser(ctx, "admin", "secret123", "Админ", auth.RoleAdmin)
	users.CreateUser(ctx, "reader", "reader123", "Читатель", auth.RoleUser)

	cfg := &config.Config{LibraryDir: libDir, DataDir: dir, Auth: mode}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html/>")}}
	srv := New(cfg, log, st, library.New(libDir), users, nil, webFS)
	f.ts = httptest.NewServer(srv.Handler)
	t.Cleanup(f.ts.Close)
	if mode != "demo" {
		f.admin = newClientLoggedIn(t, f.ts.URL, "admin", "secret123")
		f.reader = newClientLoggedIn(t, f.ts.URL, "reader", "reader123")
		// Tests must not reach external services: switch every source off.
		postJSON(t, f.admin, f.ts.URL+"/admin/settings", map[string]any{
			"enrichment": map[string]bool{"livelib": false, "google_books": false, "open_library": false},
			"similar":    map[string]bool{"fantlab": false, "tastedive": false},
		}).Body.Close()
	}
	return f
}

func do(t *testing.T, c *http.Client, method, url, body string) *http.Response {
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
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func code(t *testing.T, c *http.Client, method, url, body string) int {
	t.Helper()
	resp := do(t, c, method, url, body)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// TestRouteAccess walks every registered route with an anonymous client,
// a reader and an administrator: the auth layer must answer before any
// handler logic does. Body-dependent handlers may return 400/404 for the
// admin — what matters is that 401/403 land exactly where expected.
func TestRouteAccess(t *testing.T) {
	f := newFullServer(t, "required")
	anon := &http.Client{}
	id := itoa64(f.bookID)

	type route struct {
		method, path, body string
	}
	// Every route from server.go except login/health, with sample ids.
	userRoutes := []route{
		{"GET", "/auth/me", ""}, // 200 for everyone, but reports no user
		{"GET", "/api/v1/collections", ""},
		{"GET", "/api/v1/collections/nope", ""},
		{"GET", "/api/v1/lists", ""},
		{"GET", "/api/v1/lists/1/books", ""},
		{"GET", "/api/v1/me/reader-email", ""},
		{"GET", "/api/v1/read/" + id, ""},
		{"GET", "/api/v1/read/" + id + "/chapter/0", ""},
		{"GET", "/api/v1/read/" + id + "/img/cover.png", ""},
		{"GET", "/api/v1/read/" + id + "/progress", ""},
		{"GET", "/api/v1/sync/state", ""},
		{"GET", "/Images/covers/" + id, ""},
		{"GET", "/Images/export?ids=" + id, ""},
		{"GET", "/Images/fb2compact/" + id, ""},
		{"GET", "/Images/fb2/" + id, ""},
		{"GET", "/Images/zip/" + id, ""},
		{"GET", "/main/getBooks/getBookForm?selectedItemID=" + id, ""},
		{"GET", "/main/getBooks/getBooksByIds?ids=" + id, ""},
		{"GET", "/main/getBooks/getCatalogShelves", ""},
		{"GET", "/main/getBooks/getConfig", ""},
		{"GET", "/main/getBooks/getExternalEnrichment?bookId=1&title=x", ""},
		{"GET", "/main/getBooks/getHomeShelves", ""},
		{"GET", "/main/getBooks/getSearchAuthorBooks?selectedItemID=" + itoa64(f.authorID), ""},
		{"GET", "/main/getBooks/getSearchAuthors?search=Толстой", ""},
		{"GET", "/main/getBooks/getSearchGenres?search=", ""},
		{"GET", "/main/getBooks/getSearchSeries?search=Класс", ""},
		{"GET", "/main/getBooks/getSearchSeriesBooks?selectedItemID=" + itoa64(f.seriesID), ""},
		{"GET", "/main/getBooks/getSearchStats?search=Война", ""},
		{"GET", "/main/getBooks/getSearchTitles?search=Война", ""},
		{"GET", "/main/getBooks/getShelfBooks?shelfId=newest", ""},
		{"GET", "/main/getBooks/getSimilarBooks?bookId=" + id, ""},
		{"GET", "/opds", ""},
		{"GET", "/opds/", ""},
		{"GET", "/opds/author/" + itoa64(f.authorID), ""},
		{"GET", "/opds/authors", ""},
		{"GET", "/opds/authors/Т", ""},
		{"GET", "/opds/genre/prose_classic", ""},
		{"GET", "/opds/genres", ""},
		{"GET", "/opds/new", ""},
		{"GET", "/opds/opensearch", ""},
		{"GET", "/opds/reading", ""},
		{"GET", "/opds/search?q=Война", ""},
		{"GET", "/opds/series", ""},
		{"GET", "/opds/series/id/" + itoa64(f.seriesID), ""},
		{"GET", "/opds/series/К", ""},
		{"POST", "/api/v1/books/" + id + "/rating", `{"rating":3}`},
		{"POST", "/api/v1/books/" + id + "/send", `{}`},
		{"POST", "/api/v1/books/" + id + "/wishlist", `{"add":true}`},
		{"POST", "/api/v1/lists", `{"name":"x"}`},
		{"POST", "/api/v1/lists/1", `{"name":"y"}`},
		{"POST", "/api/v1/lists/1/books", `{"bookId":1}`},
		{"POST", "/api/v1/lists/1/books/remove", `{"bookId":1}`},
		{"POST", "/api/v1/lists/1/delete", ""},
		{"POST", "/api/v1/me/reader-email", `{"email":"a@b.c"}`},
		{"POST", "/api/v1/read/" + id + "/progress", `{"chapter":0}`},
		{"POST", "/api/v1/sync/state", `{}`},
	}
	adminRoutes := []route{
		{"GET", "/admin/import/status", ""},
		{"GET", "/admin/settings", ""},
		{"GET", "/admin/users", ""},
		{"POST", "/admin/books/" + id + "/delete", ""},
		{"POST", "/admin/books/" + id + "/restore", ""},
		{"POST", "/admin/books/upload", ""},
		{"POST", "/admin/collections/import", ""},
		{"POST", "/admin/collections/nope/delete", ""},
		{"POST", "/admin/collections/nope/match", ""},
		{"POST", "/admin/collections/sources/forbes/sync", ""},
		{"POST", "/admin/import/inpx", ""},
		{"POST", "/admin/settings", `{}`},
		{"POST", "/admin/smtp/test", ""},
		{"POST", "/admin/users", `{}`},
		{"POST", "/admin/users/999", `{}`},
		{"POST", "/admin/users/999/delete", ""},
	}

	for _, rt := range userRoutes {
		got := code(t, anon, rt.method, f.ts.URL+rt.path, rt.body)
		want := http.StatusUnauthorized
		if rt.path == "/auth/me" {
			want = http.StatusOK
		}
		if got != want {
			t.Errorf("anonymous %s %s -> %d, want %d", rt.method, rt.path, got, want)
		}
		if got := code(t, f.reader, rt.method, f.ts.URL+rt.path, rt.body); got == http.StatusUnauthorized || got == http.StatusForbidden {
			t.Errorf("reader %s %s -> %d, must be allowed through auth", rt.method, rt.path, got)
		}
	}
	for _, rt := range adminRoutes {
		if got := code(t, anon, rt.method, f.ts.URL+rt.path, rt.body); got != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s -> %d, want 401", rt.method, rt.path, got)
		}
		if got := code(t, f.reader, rt.method, f.ts.URL+rt.path, rt.body); got != http.StatusForbidden {
			t.Errorf("reader %s %s -> %d, want 403", rt.method, rt.path, got)
		}
		if got := code(t, f.admin, rt.method, f.ts.URL+rt.path, rt.body); got == http.StatusUnauthorized || got == http.StatusForbidden {
			t.Errorf("admin %s %s -> %d, must be allowed", rt.method, rt.path, got)
		}
	}

	// OPDS challenges Basic auth; browsers with a cookie are not challenged.
	resp := do(t, anon, "GET", f.ts.URL+"/opds/reading", "")
	resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Error("OPDS must send a Basic challenge")
	}
	if code(t, anon, "GET", f.ts.URL+"/api/v1/health", "") != http.StatusOK {
		t.Error("health must be public")
	}

	// Public mode: everything readable without a login, admin routes still guarded.
	pub := newFullServer(t, "public")
	for _, p := range []string{"/main/getBooks/getHomeShelves", "/opds", "/Images/fb2/" + itoa64(pub.bookID), "/api/v1/read/" + itoa64(pub.bookID)} {
		if got := code(t, anon, "GET", pub.ts.URL+p, ""); got != http.StatusOK {
			t.Errorf("public %s -> %d", p, got)
		}
	}
	if got := code(t, anon, "GET", pub.ts.URL+"/admin/users", ""); got != http.StatusUnauthorized {
		t.Errorf("public mode admin route -> %d, want 401", got)
	}
	// Without a login, user data endpoints report "no user" rather than failing.
	if got := code(t, anon, "GET", pub.ts.URL+"/api/v1/lists", ""); got != http.StatusUnauthorized {
		t.Errorf("public lists without login -> %d, want 401", got)
	}
}

func TestDemoMode(t *testing.T) {
	f := newFullServer(t, "demo")
	jar := newJar(t)
	c := &http.Client{Jar: jar}

	// The first visit provisions a guest and sets the session cookie.
	resp := do(t, c, "GET", f.ts.URL+"/main/getBooks/getHomeShelves", "")
	resp.Body.Close()
	if len(resp.Cookies()) == 0 || resp.Cookies()[0].Name != sessionCookie {
		t.Fatalf("guest cookie not set: %v", resp.Cookies())
	}
	me := getJSONWith(t, c, f.ts.URL+"/auth/me")
	user, _ := me["user"].(map[string]any)
	if user == nil || user["role"] != auth.RoleUser {
		t.Fatalf("guest user: %v", me)
	}
	// The guest keeps state across requests (same cookie, same user).
	if code(t, c, "POST", f.ts.URL+"/api/v1/books/"+itoa64(f.bookID)+"/rating", `{"rating":4}`) != http.StatusOK {
		t.Error("guest must be able to rate")
	}
	form := getJSONWith(t, c, f.ts.URL+"/main/getBooks/getBookForm?selectedItemID="+itoa64(f.bookID))
	if form["userRating"].(float64) != 4 {
		t.Errorf("guest rating not persisted: %v", form["userRating"])
	}
	if getJSONWith(t, c, f.ts.URL+"/auth/me")["user"].(map[string]any)["id"] != user["id"] {
		t.Error("guest identity must be stable")
	}
	// Library management does not exist in demo mode.
	for _, p := range []string{"/admin/users", "/admin/books/upload", "/admin/settings"} {
		resp := do(t, c, "GET", f.ts.URL+p, "")
		resp.Body.Close()
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Errorf("%s must not be an API route in demo mode", p)
		}
	}
	// Guest creation is rate-limited per IP.
	limited := false
	for i := 0; i < 30; i++ {
		if code(t, &http.Client{}, "GET", f.ts.URL+"/api/v1/health", "") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("guest provisioning must be rate-limited")
	}
	// A guest is a real session: logout works and the next visit is a new guest.
	code(t, c, "POST", f.ts.URL+"/auth/logout", "")
	if n, _ := f.users.UsersCount(context.Background()); n < 3 {
		t.Errorf("guests are real users: %d", n)
	}
}
