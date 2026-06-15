package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/enrich"
)

// enrichEnabled builds a map of enabled sources from settings.
func (s *Server) enrichEnabled(r *http.Request) map[string]bool {
	enabled := make(map[string]bool, len(enrich.Sources))
	for _, source := range enrich.Sources {
		enabled[source] = s.users.GetSetting(r.Context(), "enrich."+source, "1") == "1"
	}
	return enabled
}

// GET /main/getBooks/getExternalEnrichment?bookId&title&author&isbn
func (s *Server) handleGetEnrichment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	bookID := q.Get("bookId")
	title := strings.TrimSpace(strings.ReplaceAll(q.Get("title"), "+", " "))
	author := strings.TrimSpace(strings.ReplaceAll(q.Get("author"), "+", " "))
	if bookID == "" || title == "" {
		http.Error(w, "bookId and title are required", http.StatusBadRequest)
		return
	}
	res := s.enrich.Get(r.Context(), bookID, title, author, s.enrichEnabled(r))
	writeJSON(w, res)
}

// --- Settings (administrator only) ---

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	enrichment := map[string]bool{}
	for source, on := range s.enrichEnabled(r) {
		enrichment[source] = on
	}
	cfg := s.similarConfig(r)
	writeJSON(w, map[string]any{
		"enrichment": enrichment,
		"similar": map[string]bool{
			"fantlab":   cfg.FantLab,
			"tastedive": cfg.TasteDive,
		},
		"tastediveKey": cfg.TasteDiveKey,
		"opdsEnabled":  s.opdsEnabled(r),
	})
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enrichment   map[string]bool `json:"enrichment"`
		Similar      map[string]bool `json:"similar"`
		TasteDiveKey *string         `json:"tastediveKey"`
		OpdsEnabled  *bool           `json:"opdsEnabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	boolVal := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	for _, source := range enrich.Sources {
		if on, ok := req.Enrichment[source]; ok {
			if err := s.users.SetSetting(r.Context(), "enrich."+source, boolVal(on)); err != nil {
				s.apiError(w, err)
				return
			}
		}
	}
	for _, source := range []string{"fantlab", "tastedive"} {
		if on, ok := req.Similar[source]; ok {
			if err := s.users.SetSetting(r.Context(), "enrich.similar_"+source, boolVal(on)); err != nil {
				s.apiError(w, err)
				return
			}
		}
	}
	if req.TasteDiveKey != nil {
		if err := s.users.SetSetting(r.Context(), "tastedive_key", strings.TrimSpace(*req.TasteDiveKey)); err != nil {
			s.apiError(w, err)
			return
		}
	}
	if req.OpdsEnabled != nil {
		if err := s.users.SetSetting(r.Context(), "opds.enabled", boolVal(*req.OpdsEnabled)); err != nil {
			s.apiError(w, err)
			return
		}
	}
	s.handleSettingsGet(w, r)
}

// --- Book ratings ---

// POST /api/v1/books/{id}/rating {rating: 0..5} — 0 removes the rating.
func (s *Server) handleRateBook(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.st.BookFile(r.Context(), bookID); err != nil {
		s.apiError(w, err)
		return
	}
	var req struct {
		Rating int `json:"rating"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Rating < 0 || req.Rating > 5 {
		http.Error(w, "rating must be 0..5", http.StatusBadRequest)
		return
	}
	if err := s.users.RateBook(r.Context(), u.ID, bookID, req.Rating); err != nil {
		s.apiError(w, err)
		return
	}
	avg, count, err := s.users.BookRating(r.Context(), bookID)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"userRating":  req.Rating,
		"polkaRating": map[string]any{"rating": avg, "count": count},
	})
}
