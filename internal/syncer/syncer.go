// Package syncer implements the desktop client's sync mode with a Polka server:
// catalog proxying, an offline book cache and two-way synchronization
// of user data (last-write-wins).
package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/sqlitedrv"
)

// Config is the server connection; stored in data-dir/sync.json.
type Config struct {
	Server   string `json:"server"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

func ConfigPath(dataDir string) string { return filepath.Join(dataDir, "sync.json") }

func LoadConfig(dataDir string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(dataDir))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("sync config: server is empty")
	}
	return &cfg, nil
}

func SaveConfig(dataDir string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(dataDir), data, 0o600)
}

// RemoveConfig disables sync mode (deletes sync.json).
func RemoveConfig(dataDir string) error {
	err := os.Remove(ConfigPath(dataDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

var ErrBadCredentials = fmt.Errorf("неверный логин или пароль")

// Validate checks that the server is reachable and the credentials work.
func Validate(ctx context.Context, cfg *Config) error {
	remote, err := url.Parse(strings.TrimRight(cfg.Server, "/"))
	if err != nil || remote.Host == "" || (remote.Scheme != "http" && remote.Scheme != "https") {
		return fmt.Errorf("некорректный адрес сервера")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.Server, "/")+"/auth/me", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.Login, cfg.Password)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("сервер недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("сервер ответил HTTP %d", resp.StatusCode)
	}
	var me struct {
		User any `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return fmt.Errorf("это не сервер Полки")
	}
	if me.User == nil {
		return ErrBadCredentials
	}
	return nil
}

// OfflineBook is the card of a downloaded book.
type OfflineBook struct {
	ID          int64
	Title       string
	Authors     string
	SeriesTitle string
	SeqNumber   int
	Year        int
	LibRate     float64
	Ext         string
	Size        int64
}

type Syncer struct {
	cfg    Config
	log    *slog.Logger
	users  *auth.Service // local progress/ratings/lists
	db     *sql.DB       // offline.db: cards of downloaded books
	dir    string        // directory of offline book files
	client *http.Client
	proxy  *httputil.ReverseProxy
	remote *url.URL

	online   atomic.Bool
	lastSync atomic.Value // time.Time

	timerMu   gosync.Mutex
	syncTimer *time.Timer
}

const offlineSchema = `
CREATE TABLE IF NOT EXISTS offline_books (
	id           INTEGER PRIMARY KEY,
	title        TEXT NOT NULL,
	authors      TEXT NOT NULL DEFAULT '',
	series_title TEXT NOT NULL DEFAULT '',
	seq_number   INTEGER NOT NULL DEFAULT 0,
	year         INTEGER NOT NULL DEFAULT 0,
	lib_rate     REAL NOT NULL DEFAULT 0,
	ext          TEXT NOT NULL,
	size         INTEGER NOT NULL DEFAULT 0,
	cover        BLOB,
	cover_mime   TEXT NOT NULL DEFAULT '',
	saved_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
`

func New(dataDir string, cfg Config, users *auth.Service, log *slog.Logger) (*Syncer, error) {
	remote, err := url.Parse(strings.TrimRight(cfg.Server, "/"))
	if err != nil || remote.Host == "" {
		return nil, fmt.Errorf("bad server url %q", cfg.Server)
	}

	dir := filepath.Join(dataDir, "offline-books")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sqlitedrv.Open(filepath.Join(dataDir, "offline.db"), sqlitedrv.Options{
		WAL: true, BusyTimeout: 10000,
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(offlineSchema); err != nil {
		db.Close()
		return nil, err
	}

	s := &Syncer{
		cfg:    cfg,
		log:    log,
		users:  users,
		db:     db,
		dir:    dir,
		client: &http.Client{Timeout: 30 * time.Second},
		remote: remote,
	}
	s.lastSync.Store(time.Time{})

	s.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(remote)
			pr.Out.SetBasicAuth(cfg.Login, cfg.Password)
			pr.Out.Header.Del("Cookie") // the server does not need the local session
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.online.Store(false)
			http.Error(w, "server unavailable", http.StatusBadGateway)
		},
	}
	return s, nil
}

func (s *Syncer) Close() error { return s.db.Close() }

func (s *Syncer) Server() string { return s.cfg.Server }

func (s *Syncer) Online() bool { return s.online.Load() }

func (s *Syncer) LastSync() time.Time {
	t, _ := s.lastSync.Load().(time.Time)
	return t
}

// Proxy forwards the request to the server with the client's credentials.
func (s *Syncer) Proxy(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}

// request performs a request to the server with Basic authentication.
func (s *Syncer) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Server+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.cfg.Login, s.cfg.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.online.Store(false)
		return nil, err
	}
	return resp, nil
}

// CheckOnline polls the server and updates the status.
func (s *Syncer) CheckOnline(ctx context.Context) bool {
	resp, err := s.request(ctx, http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
	ok := resp.StatusCode == http.StatusOK
	s.online.Store(ok)
	return ok
}

// Run is the background loop: connectivity checks and periodic sync.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	for {
		wasOnline := s.Online()
		if s.CheckOnline(ctx) {
			// On reconnect and on schedule — a full synchronization.
			if !wasOnline || time.Since(s.LastSync()) > 4*time.Minute {
				if err := s.SyncNow(ctx); err != nil {
					s.log.Warn("sync", "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SyncNow performs a two-way state exchange: pull with LWW merge, then push.
func (s *Syncer) SyncNow(ctx context.Context) error {
	ownerID, err := s.ownerID(ctx)
	if err != nil {
		return err
	}

	// Pull: merge the server state into the local one (newer wins).
	resp, err := s.request(ctx, http.MethodGet, "/api/v1/sync/state", nil)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	var remote auth.SyncState
	err = json.NewDecoder(resp.Body).Decode(&remote)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("pull decode: %w", err)
	}
	if err := s.users.MergeState(ctx, ownerID, &remote); err != nil {
		return fmt.Errorf("merge: %w", err)
	}

	// Push: local state (including offline changes) to the server.
	local, err := s.users.ExportState(ctx, ownerID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(local)
	if err != nil {
		return err
	}
	resp, err = s.request(ctx, http.MethodPost, "/api/v1/sync/state", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push: HTTP %d", resp.StatusCode)
	}

	s.lastSync.Store(time.Now())
	s.log.Info("sync done",
		"progress", len(local.Progress), "ratings", len(local.Ratings), "lists", len(local.Lists))
	return nil
}

// FetchBooksByIDs requests book cards from the server.
func (s *Syncer) FetchBooksByIDs(ctx context.Context, ids []int64) ([]map[string]any, error) {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	resp, err := s.request(ctx, http.MethodGet,
		"/main/getBooks/getBooksByIds?ids="+strings.Join(parts, ","), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getBooksByIds: HTTP %d", resp.StatusCode)
	}
	var out struct {
		TitlesList []map[string]any `json:"titlesList"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.TitlesList, nil
}

// RequestSync schedules a sync a couple of seconds after a local
// change (debounce: a burst of edits results in a single exchange).
func (s *Syncer) RequestSync() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
	}
	s.syncTimer = time.AfterFunc(3*time.Second, func() {
		if !s.Online() {
			return // offline: changes will arrive once connectivity is restored
		}
		if err := s.SyncNow(context.Background()); err != nil {
			s.log.Warn("sync after change", "error", err)
		}
	})
}

func (s *Syncer) ownerID(ctx context.Context) (int64, error) {
	owner, err := s.users.EnsureLogin(ctx, "desktop", "Владелец", auth.RoleAdmin)
	if err != nil {
		return 0, err
	}
	return owner.ID, nil
}
