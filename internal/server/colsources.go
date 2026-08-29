package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/vestigiumincaligne/polka/internal/collections/sources"
)

// External collection sources (Forbes…): disabled by default, enabled in
// Management; enabled sources are crawled once a day and when enabled.
// Collections of a disabled source stay in the database but are not
// shown on the home page.

const sourcesSyncInterval = 24 * time.Hour

func sourceSettingKey(id string) string { return "collections.source." + id }

// sourceEnabled reports whether the source is enabled (off by default).
func (s *Server) sourceEnabled(ctx context.Context, id string) bool {
	return s.users.GetSetting(ctx, sourceSettingKey(id), "0") == "1"
}

// disabledOrigins returns the origins of collections currently hidden.
func (s *Server) disabledOrigins(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for _, src := range sources.All() {
		if !s.sourceEnabled(ctx, src.ID()) {
			out[src.ID()] = true
		}
	}
	return out
}

type sourcesState struct {
	mu      sync.Mutex
	running bool
	last    map[string]sourceRun // by source id
}

type sourceRun struct {
	At    string `json:"at"`
	Added int    `json:"added"`
	Error string `json:"error,omitempty"`
}

// sourcesLoop crawls enabled sources daily (started at boot; the first
// crawl is delayed by a minute so as not to interfere with startup).
func (s *Server) sourcesLoop(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.syncSources(ctx, "")
		timer.Reset(sourcesSyncInterval)
	}
}

// syncSources crawls the enabled sources (or one, if id is given)
// and loads new collections. Calling it again during a crawl is a no-op.
func (s *Server) syncSources(ctx context.Context, only string) {
	if s.cols == nil {
		return
	}
	s.srcs.mu.Lock()
	if s.srcs.running {
		s.srcs.mu.Unlock()
		return
	}
	s.srcs.running = true
	if s.srcs.last == nil {
		s.srcs.last = map[string]sourceRun{}
	}
	s.srcs.mu.Unlock()
	defer func() {
		s.srcs.mu.Lock()
		s.srcs.running = false
		s.srcs.mu.Unlock()
	}()

	for _, src := range sources.All() {
		id := src.ID()
		if only != "" && only != id {
			continue
		}
		if !s.sourceEnabled(ctx, id) {
			continue
		}
		run := sourceRun{At: time.Now().UTC().Format(time.RFC3339)}
		known, err := s.cols.SlugsByOrigin(ctx, id)
		if err == nil {
			run.Added, err = s.importFromSource(ctx, src, known)
		}
		if err != nil {
			run.Error = err.Error()
			s.log.Warn("collections source", "source", id, "error", err)
		} else {
			s.log.Info("collections source synced", "source", id, "added", run.Added)
		}
		s.srcs.mu.Lock()
		s.srcs.last[id] = run
		s.srcs.mu.Unlock()
	}
}

func (s *Server) importFromSource(ctx context.Context, src sources.Source, known map[string]bool) (int, error) {
	files, err := src.Fetch(ctx, known)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, f := range files {
		if s.cols.Hidden(ctx, f.Slug) {
			continue // the administrator deleted this collection
		}
		if _, err := s.cols.ImportFrom(ctx, f, src.ID()); err != nil {
			return added, err
		}
		if _, err := s.cols.Match(ctx, s.st, f.Slug); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// sourcesJSON returns the sources' state for settings.
func (s *Server) sourcesJSON(ctx context.Context) []map[string]any {
	s.srcs.mu.Lock()
	defer s.srcs.mu.Unlock()
	var out []map[string]any
	for _, src := range sources.All() {
		j := map[string]any{
			"id":      src.ID(),
			"enabled": s.sourceEnabled(ctx, src.ID()),
			"running": s.srcs.running,
		}
		if run, ok := s.srcs.last[src.ID()]; ok {
			j["last"] = run
		}
		out = append(out, j)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

// POST /admin/collections/sources/{id}/sync — crawl the source now
// (in the background; the result is visible in settings).
func (s *Server) handleSourceSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if sources.ByID(id) == nil {
		http.NotFound(w, r)
		return
	}
	if !s.sourceEnabled(r.Context(), id) {
		http.Error(w, "source is disabled", http.StatusConflict)
		return
	}
	go s.syncSources(context.Background(), id)
	writeJSON(w, map[string]any{"ok": true})
}
