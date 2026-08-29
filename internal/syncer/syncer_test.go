package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

// fakeServer imitates the parts of a Polka server the syncer talks to:
// health, /auth/me, sync state, book cards, files and covers.
type fakeServer struct {
	*httptest.Server
	login, password string

	mu       sync.Mutex
	state    auth.SyncState // what the "server" holds
	pushes   int            // POST /api/v1/sync/state calls
	lastAuth string         // Authorization header of the last request
	lastCk   string         // Cookie header of the last request
	down     atomic.Bool    // simulate an outage (HTTP 503 everywhere)
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{login: "owner", password: "secret"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if l, p, ok := r.BasicAuth(); !ok || l != fs.login || p != fs.password {
			w.Write([]byte(`{"user":null}`))
			return
		}
		w.Write([]byte(`{"user":{"login":"owner"}}`))
	})
	mux.HandleFunc("/api/v1/sync/state", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if r.Method == http.MethodPost {
			var in auth.SyncState
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			fs.state = in
			fs.pushes++
			w.Write([]byte(`{"ok":true}`))
			return
		}
		json.NewEncoder(w).Encode(fs.state)
	})
	mux.HandleFunc("/main/getBooks/getBooksByIds", func(w http.ResponseWriter, r *http.Request) {
		var list []map[string]any
		for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
			list = append(list, map[string]any{"BookID": atoi(id), "Title": "Book " + id})
		}
		json.NewEncoder(w).Encode(map[string]any{"titlesList": list})
	})
	mux.HandleFunc("/main/getBooks/getBookForm", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("selectedItemID") != "7" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"bookForm":{"Title":"Война и мир","AuthorsNames":"Толстой Лев","LibRate":4.5,"BookSize":12,"Ext":".fb2"},
			"series":[{"SeriesTitle":"Классика","SeqNumber":1}]}`))
	})
	mux.HandleFunc("/Images/fb2/7", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<FictionBook/>"))
	})
	mux.HandleFunc("/Images/covers/7", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNGDATA"))
	})
	mux.HandleFunc("/main/getBooks/getHomeShelves", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"shelves":[{"id":"newest"}]}`))
	})
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.lastAuth = r.Header.Get("Authorization")
		fs.lastCk = r.Header.Get("Cookie")
		fs.mu.Unlock()
		if fs.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func atoi(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}

func (fs *fakeServer) config() Config {
	return Config{Server: fs.URL, Login: fs.login, Password: fs.password}
}

// newSyncer builds a syncer with its own data dir and users.db.
func newSyncer(t *testing.T, cfg Config) (*Syncer, *auth.Service, string) {
	t.Helper()
	dir := t.TempDir()
	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(dir, cfg, users, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, users, dir
}

func TestConfigFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadConfig(dir); err == nil {
		t.Error("missing sync.json must be an error")
	}
	cfg := &Config{Server: "http://polka.local:12791/", Login: "me", Password: "pw"}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(ConfigPath(dir)); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("sync.json should be private (0600): %v %v", st, err)
	}
	got, err := LoadConfig(dir)
	if err != nil || *got != *cfg {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	os.WriteFile(ConfigPath(dir), []byte(`{"login":"x"}`), 0o600)
	if _, err := LoadConfig(dir); err == nil {
		t.Error("empty server must be rejected")
	}
	if err := RemoveConfig(dir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfig(dir); err != nil {
		t.Errorf("removing twice must be a no-op: %v", err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Error("config should be gone")
	}
}

func TestValidate(t *testing.T) {
	fs := newFakeServer(t)
	ctx := context.Background()

	if err := Validate(ctx, &Config{Server: "polka.local"}); err == nil {
		t.Error("url without scheme must be rejected")
	}
	if err := Validate(ctx, &Config{Server: "ftp://polka.local"}); err == nil {
		t.Error("non-http scheme must be rejected")
	}
	cfg := fs.config()
	if err := Validate(ctx, &cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}
	// A trailing slash is tolerated.
	cfg = fs.config()
	cfg.Server += "/"
	if err := Validate(ctx, &cfg); err != nil {
		t.Errorf("trailing slash: %v", err)
	}
	cfg = fs.config()
	cfg.Password = "wrong"
	if err := Validate(ctx, &cfg); err != ErrBadCredentials {
		t.Errorf("bad password: %v, want ErrBadCredentials", err)
	}
	fs.down.Store(true)
	cfg = fs.config()
	if err := Validate(ctx, &cfg); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("server error: %v", err)
	}
	fs.down.Store(false)

	// Something that answers 200 but is not Polka.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>hello</html>"))
	}))
	defer other.Close()
	if err := Validate(ctx, &Config{Server: other.URL, Login: "a", Password: "b"}); err == nil {
		t.Error("non-Polka server must be rejected")
	}
	// Unreachable host.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	if err := Validate(ctx, &Config{Server: dead.URL, Login: "a", Password: "b"}); err == nil {
		t.Error("unreachable server must be an error")
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	users := &auth.Service{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := New(t.TempDir(), Config{Server: "not a url"}, users, log); err == nil {
		t.Error("bad url must be rejected")
	}
}

func TestOnlineAndProxy(t *testing.T) {
	fs := newFakeServer(t)
	s, _, _ := newSyncer(t, fs.config())
	ctx := context.Background()

	if s.Online() {
		t.Error("must start offline until the first check")
	}
	if !s.CheckOnline(ctx) || !s.Online() {
		t.Fatal("server is up, CheckOnline should succeed")
	}
	if s.Server() != fs.URL {
		t.Errorf("Server() = %q", s.Server())
	}

	// Proxy: the request reaches the server with Basic auth and without
	// the local session cookie.
	req := httptest.NewRequest(http.MethodGet, "/main/getBooks/getHomeShelves", nil)
	req.Header.Set("Cookie", "polka_session=local")
	rec := httptest.NewRecorder()
	s.Proxy(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "newest") {
		t.Fatalf("proxy: %d %s", rec.Code, rec.Body.String())
	}
	fs.mu.Lock()
	authHdr, cookie := fs.lastAuth, fs.lastCk
	fs.mu.Unlock()
	if !strings.HasPrefix(authHdr, "Basic ") {
		t.Errorf("proxy must add Basic auth, got %q", authHdr)
	}
	if cookie != "" {
		t.Errorf("proxy must strip the local cookie, got %q", cookie)
	}

	// Outage: the server answers 503 → offline; a dead socket → 502 from the proxy.
	fs.down.Store(true)
	if s.CheckOnline(ctx) || s.Online() {
		t.Error("503 must mark the syncer offline")
	}
	fs.down.Store(false)
	if !s.CheckOnline(ctx) {
		t.Fatal("back online")
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	sDead, _, _ := newSyncer(t, Config{Server: dead.URL, Login: "a", Password: "b"})
	sDead.online.Store(true)
	rec = httptest.NewRecorder()
	sDead.Proxy(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusBadGateway || sDead.Online() {
		t.Errorf("dead server: code %d online %v", rec.Code, sDead.Online())
	}
	if sDead.CheckOnline(ctx) {
		t.Error("dead server cannot be online")
	}
}

func TestSyncNow(t *testing.T) {
	fs := newFakeServer(t)
	s, users, _ := newSyncer(t, fs.config())
	ctx := context.Background()

	if !s.LastSync().IsZero() {
		t.Error("LastSync must be zero before the first sync")
	}

	// Local: progress in book 1, rating for book 2, a custom list.
	owner, _ := users.EnsureLogin(ctx, "desktop", "Владелец", auth.RoleAdmin)
	users.SaveProgress(ctx, owner.ID, 1, auth.Progress{Chapter: 3, Overall: 0.3})
	users.RateBook(ctx, owner.ID, 2, 5)
	list, _ := users.CreateList(ctx, owner.ID, "Отпуск")
	users.AddToList(ctx, owner.ID, list.ID, 2)

	// Server: newer progress for book 1, progress for book 9 the client never saw.
	fs.mu.Lock()
	fs.state = auth.SyncState{Progress: []auth.ProgressState{
		{BookID: 1, Chapter: 8, Overall: 0.8, UpdatedAt: "2099-01-01 00:00:00"},
		{BookID: 9, Chapter: 1, Overall: 0.1, UpdatedAt: "2099-01-01 00:00:00"},
	}}
	fs.mu.Unlock()

	if err := s.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	if s.LastSync().IsZero() {
		t.Error("LastSync must be set after a successful sync")
	}

	// Pull: the server's newer progress won locally, book 9 appeared.
	if p, _ := users.GetProgress(ctx, owner.ID, 1); p.Chapter != 8 {
		t.Errorf("book 1 chapter = %d, want 8 (server newer)", p.Chapter)
	}
	if p, err := users.GetProgress(ctx, owner.ID, 9); err != nil || p.Chapter != 1 {
		t.Errorf("book 9 progress: %+v %v", p, err)
	}
	// Push: the server now holds the merged state including local-only data.
	fs.mu.Lock()
	pushed, pushes := fs.state, fs.pushes
	fs.mu.Unlock()
	if pushes != 1 {
		t.Errorf("pushes = %d, want 1", pushes)
	}
	if len(pushed.Progress) != 2 || len(pushed.Ratings) != 1 || pushed.Ratings[0].BookID != 2 {
		t.Errorf("pushed state: %+v", pushed)
	}
	found := false
	for _, l := range pushed.Lists {
		if l.Name == "Отпуск" && len(l.Books) == 1 && l.Books[0].BookID == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("custom list not pushed: %+v", pushed.Lists)
	}

	// Server failure → error, LastSync unchanged.
	last := s.LastSync()
	fs.down.Store(true)
	if err := s.SyncNow(ctx); err == nil {
		t.Error("sync against a 503 server must fail")
	}
	if !s.LastSync().Equal(last) {
		t.Error("LastSync must not move on failure")
	}
}

func TestRequestSyncDebounce(t *testing.T) {
	fs := newFakeServer(t)
	s, _, _ := newSyncer(t, fs.config())
	old := syncDebounce
	syncDebounce = 50 * time.Millisecond
	t.Cleanup(func() { syncDebounce = old })

	// Offline: nothing happens.
	s.RequestSync()
	time.Sleep(150 * time.Millisecond)
	fs.mu.Lock()
	n := fs.pushes
	fs.mu.Unlock()
	if n != 0 {
		t.Fatalf("offline RequestSync must not push, got %d", n)
	}

	// Online: a burst of requests collapses into a single sync.
	s.CheckOnline(context.Background())
	for i := 0; i < 5; i++ {
		s.RequestSync()
		time.Sleep(5 * time.Millisecond)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		fs.mu.Lock()
		n = fs.pushes
		fs.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // let any stray timers fire
	fs.mu.Lock()
	n = fs.pushes
	fs.mu.Unlock()
	if n != 1 {
		t.Errorf("pushes = %d, want exactly 1 (debounced)", n)
	}
}

func TestFetchBooksByIDs(t *testing.T) {
	fs := newFakeServer(t)
	s, _, _ := newSyncer(t, fs.config())
	books, err := s.FetchBooksByIDs(context.Background(), []int64{3, 5})
	if err != nil || len(books) != 2 || books[1]["Title"] != "Book 5" {
		t.Fatalf("books: %v %v", books, err)
	}
	fs.down.Store(true)
	if _, err := s.FetchBooksByIDs(context.Background(), []int64{3}); err == nil {
		t.Error("503 must be an error")
	}
}

func TestOfflineCache(t *testing.T) {
	fs := newFakeServer(t)
	s, _, dir := newSyncer(t, fs.config())
	ctx := context.Background()

	if _, err := s.OfflineBook(ctx, 7); err != ErrOffline {
		t.Errorf("unknown book: %v, want ErrOffline", err)
	}
	if _, _, err := s.OfflineCover(ctx, 7); err != ErrOffline {
		t.Errorf("unknown cover: %v", err)
	}
	if _, _, err := s.OpenOffline(ctx, 7); err != ErrOffline {
		t.Errorf("unknown file: %v", err)
	}
	if err := s.RemoveOffline(ctx, 7); err != ErrOffline {
		t.Errorf("remove unknown: %v", err)
	}

	// Unknown on the server: nothing is cached, no stray files.
	if _, err := s.MakeOffline(ctx, 8); err == nil {
		t.Error("book 8 does not exist on the server")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "offline-books")); len(entries) != 0 {
		t.Errorf("no files expected after a failed download: %v", entries)
	}

	b, err := s.MakeOffline(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Война и мир" || b.Authors != "Толстой Лев" || b.Ext != "fb2" ||
		b.SeriesTitle != "Классика" || b.SeqNumber != 1 || b.LibRate != 4.5 || b.Size != int64(len("<FictionBook/>")) {
		t.Errorf("card: %+v", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "offline-books", "7.fb2")); err != nil {
		t.Errorf("book file: %v", err)
	}
	if got, _ := s.OfflineBook(ctx, 7); got.Title != b.Title {
		t.Errorf("OfflineBook: %+v", got)
	}
	if data, mime, err := s.OfflineCover(ctx, 7); err != nil || string(data) != "PNGDATA" || mime != "image/png" {
		t.Errorf("cover: %q %q %v", data, mime, err)
	}
	rc, ob, err := s.OpenOffline(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(rc)
	rc.Close()
	if string(content) != "<FictionBook/>" || ob.ID != 7 {
		t.Errorf("file content: %q", content)
	}
	if list, _ := s.OfflineBooks(ctx); len(list) != 1 || list[0].ID != 7 {
		t.Errorf("list: %+v", list)
	}

	// Re-download updates the card in place (no duplicate rows).
	if _, err := s.MakeOffline(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.OfflineBooks(ctx); len(list) != 1 {
		t.Errorf("duplicate after re-download: %+v", list)
	}

	// Offline books survive without the server.
	fs.down.Store(true)
	if _, err := s.OfflineBook(ctx, 7); err != nil {
		t.Errorf("cache must work offline: %v", err)
	}
	if _, err := s.MakeOffline(ctx, 7); err == nil {
		t.Error("download while the server is down must fail")
	}

	if err := s.RemoveOffline(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "offline-books", "7.fb2")); !os.IsNotExist(err) {
		t.Error("file must be deleted with the card")
	}
	if list, _ := s.OfflineBooks(ctx); len(list) != 0 {
		t.Errorf("list after remove: %+v", list)
	}
}

func TestRunSyncsOnReconnect(t *testing.T) {
	fs := newFakeServer(t)
	s, _, _ := newSyncer(t, fs.config())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	// The first iteration checks connectivity and, being freshly online, syncs.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		n := fs.pushes
		fs.mu.Unlock()
		if n == 1 && s.Online() && !s.LastSync().IsZero() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	fs.mu.Lock()
	n := fs.pushes
	fs.mu.Unlock()
	if n != 1 {
		t.Errorf("Run must sync once on the first successful check, pushes = %d", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run must return when the context is cancelled")
	}
}
