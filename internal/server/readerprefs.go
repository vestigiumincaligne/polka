package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Per-user reader preferences (typography, theme). Stored server-side so
// they follow the user between devices; the frontend keeps a localStorage
// copy as a fallback for the public mode and while offline.

// readerPrefs is the validated shape; pointers distinguish "not sent"
// from zero values on partial updates.
type readerPrefs struct {
	FontStep  *int    `json:"fontStep,omitempty"`  // index into the font size scale
	Theme     *string `json:"theme,omitempty"`     // paper | sepia | night
	Font      *string `json:"font,omitempty"`      // serif | sans
	LineStep  *int    `json:"lineStep,omitempty"`  // line height: 0 compact, 1 normal, 2 loose
	WidthStep *int    `json:"widthStep,omitempty"` // text measure: 0 narrow, 1 normal, 2 wide
}

func clampStep(v *int, max int) {
	if v == nil {
		return
	}
	if *v < 0 {
		*v = 0
	}
	if *v > max {
		*v = max
	}
}

func oneOf(v *string, allowed ...string) bool {
	if v == nil {
		return true
	}
	for _, a := range allowed {
		if *v == a {
			return true
		}
	}
	return false
}

func readerPrefsKey(userID int64) string { return fmt.Sprintf("reader_prefs:%d", userID) }

// GET/POST /api/v1/me/reader-prefs — the current user's reader settings.
// POST merges the sent fields into the stored ones.
func (s *Server) handleReaderPrefs(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	stored := readerPrefs{}
	if raw := s.users.GetSetting(r.Context(), readerPrefsKey(u.ID), ""); raw != "" {
		json.Unmarshal([]byte(raw), &stored)
	}
	if r.Method == http.MethodPost {
		var req readerPrefs
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if !oneOf(req.Theme, "paper", "sepia", "night") || !oneOf(req.Font, "serif", "sans") {
			http.Error(w, "invalid value", http.StatusBadRequest)
			return
		}
		clampStep(req.FontStep, 3)
		clampStep(req.LineStep, 2)
		clampStep(req.WidthStep, 2)
		merge := func(dst **int, src *int) {
			if src != nil {
				*dst = src
			}
		}
		merge(&stored.FontStep, req.FontStep)
		merge(&stored.LineStep, req.LineStep)
		merge(&stored.WidthStep, req.WidthStep)
		if req.Theme != nil {
			stored.Theme = req.Theme
		}
		if req.Font != nil {
			stored.Font = req.Font
		}
		raw, _ := json.Marshal(stored)
		if err := s.users.SetSetting(r.Context(), readerPrefsKey(u.ID), string(raw)); err != nil {
			s.apiError(w, err)
			return
		}
	}
	writeJSON(w, stored)
}
