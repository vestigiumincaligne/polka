// Package server wires the HTTP API and the embedded SPA together.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/enrich"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
	"github.com/vestigiumincaligne/polka/internal/syncer"
)

type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	st      *store.Store
	lib     *library.Library // nil if library-dir is not set
	users   *auth.Service
	desktop *auth.User     // non-nil in desktop mode: owner auto-login
	sync    *syncer.Syncer // non-nil in server synchronization mode
	enrich  *enrich.Provider
	imp     importState // state of the background inpx web import
	reader  readerCache // parsed books for online reading
	basic   basicCache  // verified Basic credentials (OPDS)
	genres  genreCache  // genre counters for search
}

func New(cfg *config.Config, log *slog.Logger, st *store.Store, lib *library.Library, users *auth.Service, sync *syncer.Syncer, webFS fs.FS) *http.Server {
	s := &Server{
		cfg: cfg, log: log, st: st, lib: lib, users: users, sync: sync,
		enrich: enrich.New(filepath.Join(cfg.DataDir, "enrichment.json")),
	}
	if cfg.Auth == "desktop" {
		owner, err := users.EnsureLogin(context.Background(), "desktop", "Владелец", auth.RoleAdmin)
		if err != nil {
			log.Error("desktop user", "error", err)
		} else {
			s.desktop = owner
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Authentication
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /auth/me", s.handleMe)

	// User management (administrator only).
	// Absent in desktop mode (single user) and in demo mode.
	if s.desktop == nil && !s.demoMode() {
		mux.HandleFunc("GET /admin/users", s.adminOnly(s.handleUsersList))
		mux.HandleFunc("POST /admin/users", s.adminOnly(s.handleUserCreate))
		mux.HandleFunc("POST /admin/users/{id}", s.adminOnly(s.handleUserUpdate))
		mux.HandleFunc("POST /admin/users/{id}/delete", s.adminOnly(s.handleUserDelete))
	}

	if s.desktop != nil {
		// Server connection configuration from the client UI.
		s.registerDesktopConfigRoutes(mux)
	}

	if s.sync != nil {
		// Sync mode: catalog and reading go through proxy/offline cache,
		// library management and settings are proxied to the server.
		s.registerSyncRoutes(mux)
		for _, p := range []string{
			"POST /admin/books/upload",
			"POST /admin/books/{id}/delete",
			"POST /admin/books/{id}/restore",
			"POST /admin/import/inpx",
			"GET /admin/import/status",
			"GET /admin/settings",
			"POST /admin/settings",
		} {
			mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
				if !s.sync.Online() {
					http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
					return
				}
				s.sync.Proxy(w, r)
			})
		}
	} else if !s.demoMode() {
		// Library management (administrator only)
		mux.HandleFunc("POST /admin/books/upload", s.adminOnly(s.handleBookUpload))
		mux.HandleFunc("POST /admin/books/{id}/delete", s.adminOnly(s.handleBookSetDeleted(true)))
		mux.HandleFunc("POST /admin/books/{id}/restore", s.adminOnly(s.handleBookSetDeleted(false)))
		mux.HandleFunc("POST /admin/import/inpx", s.adminOnly(s.handleImportInpx))
		mux.HandleFunc("GET /admin/import/status", s.adminOnly(s.handleImportStatus))

		// Bulk book export (any logged-in user)
		mux.HandleFunc("GET /Images/export", s.protected(s.handleExport))

		// OPDS catalog for readers (HTTP Basic)
		mux.HandleFunc("GET /opds", s.opdsAuth(s.handleOpdsRoot))
		mux.HandleFunc("GET /opds/{$}", s.opdsAuth(s.handleOpdsRoot))
		mux.HandleFunc("GET /opds/opensearch", s.opdsAuth(s.handleOpdsOpenSearch))
		mux.HandleFunc("GET /opds/new", s.opdsAuth(s.handleOpdsNew))
		mux.HandleFunc("GET /opds/genres", s.opdsAuth(s.handleOpdsGenres))
		mux.HandleFunc("GET /opds/genre/{code}", s.opdsAuth(s.handleOpdsGenre))
		mux.HandleFunc("GET /opds/search", s.opdsAuth(s.handleOpdsSearch))
		mux.HandleFunc("GET /opds/reading", s.opdsAuth(s.handleOpdsReading))

		// Online reading
		mux.HandleFunc("GET /api/v1/read/{id}", s.protected(s.handleReadMeta))
		mux.HandleFunc("GET /api/v1/read/{id}/chapter/{n}", s.protected(s.handleReadChapter))
		mux.HandleFunc("GET /api/v1/read/{id}/img/{imgId}", s.protected(s.handleReadImage))
		mux.HandleFunc("GET /api/v1/read/{id}/progress", s.protected(s.handleReadProgress))
		mux.HandleFunc("POST /api/v1/read/{id}/progress", s.protected(s.handleReadProgress))

		// Frontend v2 contract: main/getBooks/*
		mux.HandleFunc("GET /main/getBooks/getConfig", s.protected(s.handleGetConfig))
		mux.HandleFunc("GET /main/getBooks/getHomeShelves", s.protected(s.handleGetHomeShelves))
		mux.HandleFunc("GET /main/getBooks/getCatalogShelves", s.protected(s.handleGetCatalogShelves))
		mux.HandleFunc("GET /main/getBooks/getShelfBooks", s.protected(s.handleGetShelfBooks))
		mux.HandleFunc("GET /main/getBooks/getSearchStats", s.protected(s.handleGetSearchStats))
		mux.HandleFunc("GET /main/getBooks/getSearchTitles", s.protected(s.handleGetSearchTitles))
		mux.HandleFunc("GET /main/getBooks/getSearchAuthors", s.protected(s.handleGetSearchAuthors))
		mux.HandleFunc("GET /main/getBooks/getSearchSeries", s.protected(s.handleGetSearchSeries))
		mux.HandleFunc("GET /main/getBooks/getSearchGenres", s.protected(s.handleGetSearchGenres))
		mux.HandleFunc("GET /main/getBooks/getSearchAuthorBooks", s.protected(s.handleGetAuthorBooks))
		mux.HandleFunc("GET /main/getBooks/getSearchSeriesBooks", s.protected(s.handleGetSeriesBooks))
		mux.HandleFunc("GET /main/getBooks/getBookForm", s.protected(s.handleGetBookForm))
		mux.HandleFunc("GET /main/getBooks/getBooksByIds", s.protected(s.handleGetBooksByIDs))
		mux.HandleFunc("GET /main/getBooks/getSimilarBooks", s.protected(s.handleGetSimilarBooks))
		mux.HandleFunc("GET /main/getBooks/getExternalEnrichment", s.protected(s.handleGetEnrichment))

		// Covers and book files
		mux.HandleFunc("GET /Images/covers/{id}", s.protected(s.handleCover))
		mux.HandleFunc("GET /Images/fb2/{id}", s.protected(s.handleBookDownload))
		mux.HandleFunc("GET /Images/zip/{id}", s.protected(s.handleBookZip))
		mux.HandleFunc("GET /Images/fb2compact/{id}", s.protected(s.handleBookCompact))

		// Settings (administrator only)
		if !s.demoMode() {
			mux.HandleFunc("GET /admin/settings", s.adminOnly(s.handleSettingsGet))
			mux.HandleFunc("POST /admin/settings", s.adminOnly(s.handleSettingsSave))
		}

		// User data state for desktop clients
		mux.HandleFunc("GET /api/v1/sync/state", s.protected(s.handleSyncExport))
		mux.HandleFunc("POST /api/v1/sync/state", s.protected(s.handleSyncMerge))
	}

	// Ratings are local in every mode (synchronized via state)
	mux.HandleFunc("POST /api/v1/books/{id}/rating", s.protected(s.maybeSyncAfter(s.handleRateBook)))

	// Reading lists are local in every mode
	mux.HandleFunc("GET /api/v1/lists", s.protected(s.handleListsGet))
	mux.HandleFunc("POST /api/v1/lists", s.protected(s.maybeSyncAfter(s.handleListCreate)))
	mux.HandleFunc("POST /api/v1/lists/{id}", s.protected(s.maybeSyncAfter(s.handleListRename)))
	mux.HandleFunc("POST /api/v1/lists/{id}/delete", s.protected(s.maybeSyncAfter(s.handleListDelete)))
	mux.HandleFunc("GET /api/v1/lists/{id}/books", s.protected(s.handleListBooks))
	mux.HandleFunc("POST /api/v1/lists/{id}/books", s.protected(s.maybeSyncAfter(s.handleListAddBook)))
	mux.HandleFunc("POST /api/v1/lists/{id}/books/remove", s.protected(s.maybeSyncAfter(s.handleListRemoveBook)))
	mux.HandleFunc("POST /api/v1/books/{id}/wishlist", s.protected(s.maybeSyncAfter(s.handleWishlistToggle)))

	mux.Handle("/", spaHandler(webFS))

	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.logRequests(s.maybeDemo(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"status": "ok"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	count, err := s.st.BookCount(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"collectionName": s.st.GetMeta(r.Context(), "collection_name", "Полка"),
		"numberOfBooks":  count,
	})
}

func init() {
	// Go does not know .webmanifest out of the box.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// spaHandler serves the embedded frontend, falling back to index.html
// for unknown paths so client-side routing keeps working.
func spaHandler(webFS fs.FS) http.Handler {
	fileServer := http.FileServerFS(webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(webFS, r.URL.Path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, webFS, "index.html")
	})
}
