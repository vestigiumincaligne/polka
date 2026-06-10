package server

import (
	"encoding/json"
	"net/http"

	"github.com/vestigiumincaligne/polka/internal/auth"
)

// GET /api/v1/sync/state — state of user data
// (progress, ratings, lists) with timestamps for LWW merging.
func (s *Server) handleSyncExport(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	state, err := s.users.ExportState(r.Context(), u.ID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, state)
}

// POST /api/v1/sync/state — accept the client's state (LWW merge).
func (s *Server) handleSyncMerge(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}
	var in auth.SyncState
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := s.users.MergeState(r.Context(), u.ID, &in); err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
