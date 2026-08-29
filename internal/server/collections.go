package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/collections"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// Collection shelves: ids of the form "collection_<slug>", so that getShelfBooks
// and the /shelf/… page work without a separate API.
const collectionShelfPrefix = "collection_"

func (s *Server) collectionsCount(ctx context.Context) int {
	if s.cols == nil {
		return 0
	}
	return s.cols.Count(ctx)
}

// rematchCollections recomputes the matching of all collections against
// the library (after startup and after a catalog import).
func (s *Server) rematchCollections(ctx context.Context) {
	if s.cols == nil {
		return
	}
	stats, err := s.cols.MatchAll(ctx, s.st)
	if err != nil {
		s.log.Warn("collections match", "error", err)
		return
	}
	for _, st := range stats {
		s.log.Info("collection matched", "slug", st.Slug, "matched", st.Matched, "total", st.Total)
	}
}

func collectionJSON(c collections.Collection) map[string]any {
	return map[string]any{
		"slug":        c.Slug,
		"title":       c.Title,
		"description": c.Description,
		"source":      c.Source,
		"url":         c.URL,
		"total":       c.Total,
		"matched":     c.Matched,
		"matchedAt":   c.MatchedAt,
		"bundled":     c.Bundled(),
		"origin":      c.Origin,
		"shelfId":     collectionShelfPrefix + c.Slug,
	}
}

// collectionShelves returns the home page shelves: collections with at least one match.
func (s *Server) collectionShelves(r *http.Request, limit int) []map[string]any {
	if s.cols == nil {
		return nil
	}
	list, err := s.cols.List(r.Context())
	if err != nil {
		s.log.Warn("collections", "error", err)
		return nil
	}
	disabled := s.disabledOrigins(r.Context())
	var out []map[string]any
	for _, c := range list {
		if c.Matched == 0 || disabled[c.Origin] {
			continue
		}
		books, _, err := s.collectionShelfBooks(r.Context(), c.Slug, limit+1, 0)
		if err != nil || len(books) == 0 {
			continue
		}
		hasMore := len(books) > limit
		if hasMore {
			books = books[:limit]
		}
		out = append(out, map[string]any{
			"id":       collectionShelfPrefix + c.Slug,
			"title":    c.Title,
			"subtitle": tr(reqLang(r), "shelf.collection.sub", c.Matched, c.Total),
			"slug":     c.Slug,
			"books":    booksJSON(books),
			"hasMore":  hasMore,
		})
	}
	return out
}

// collectionShelfBooks returns a page of the collection's books in list order.
func (s *Server) collectionShelfBooks(ctx context.Context, slug string, limit, offset int) ([]store.Book, string, error) {
	if s.cols == nil {
		return nil, "", store.ErrNotFound
	}
	c, err := s.cols.Get(ctx, slug)
	if errors.Is(err, collections.ErrNotFound) {
		return nil, "", store.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	ids, err := s.cols.BookIDs(ctx, c.ID, limit, offset)
	if err != nil || len(ids) == 0 {
		return nil, c.Title, err
	}
	books, err := s.st.BooksByIDs(ctx, ids)
	return books, c.Title, err
}

func (s *Server) collectionsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, collections.ErrNotFound):
		http.Error(w, "collection not found", http.StatusNotFound)
	case errors.Is(err, collections.ErrInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		s.log.Error("collections", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// GET /api/v1/collections — all collections with counters.
func (s *Server) handleCollectionsList(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if s.cols != nil {
		list, err := s.cols.List(r.Context())
		if err != nil {
			s.collectionsError(w, err)
			return
		}
		for _, c := range list {
			out = append(out, collectionJSON(c))
		}
	}
	writeJSON(w, map[string]any{"collections": out})
}

// GET /api/v1/collections/{slug} — a collection with the full list: matched
// entries carry the book card, unmatched ones only the author and title.
func (s *Server) handleCollectionGet(w http.ResponseWriter, r *http.Request) {
	if s.cols == nil {
		http.Error(w, "collection not found", http.StatusNotFound)
		return
	}
	c, err := s.cols.Get(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	items, err := s.cols.Items(r.Context(), c.ID)
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	var ids []int64
	for _, it := range items {
		if it.BookID != 0 {
			ids = append(ids, it.BookID)
		}
	}
	byID := map[int64]store.Book{}
	if len(ids) > 0 {
		books, err := s.st.BooksByIDs(r.Context(), ids)
		if err != nil {
			s.apiError(w, err)
			return
		}
		for _, b := range books {
			byID[b.ID] = b
		}
	}
	outItems := make([]map[string]any, 0, len(items))
	for _, it := range items {
		j := map[string]any{
			"position": it.Position,
			"title":    it.Title,
			"author":   it.Author,
			"isbn":     it.ISBN,
			"year":     it.Year,
			"note":     it.Note,
		}
		if b, ok := byID[it.BookID]; ok {
			j["book"] = bookJSON(b)
			j["match"] = it.MatchKind
		}
		outItems = append(outItems, j)
	}
	out := collectionJSON(*c)
	out["items"] = outItems
	writeJSON(w, out)
}

// POST /admin/collections/import — a collection JSON file (multipart "file"
// or the request body). A collection with the same slug is replaced.
func (s *Server) handleCollectionImport(w http.ResponseWriter, r *http.Request) {
	if s.cols == nil {
		http.Error(w, "collections unavailable", http.StatusServiceUnavailable)
		return
	}
	var body io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		defer f.Close()
		body = f
	}
	parsed, err := collections.Parse(io.LimitReader(body, 8<<20))
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	c, err := s.cols.Import(r.Context(), parsed)
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	if _, err := s.cols.Match(r.Context(), s.st, c.Slug); err != nil {
		s.collectionsError(w, err)
		return
	}
	c, err = s.cols.Get(r.Context(), c.Slug)
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	writeJSON(w, collectionJSON(*c))
}

// POST /admin/collections/{slug}/match — recompute the matching.
func (s *Server) handleCollectionMatch(w http.ResponseWriter, r *http.Request) {
	if s.cols == nil {
		http.Error(w, "collections unavailable", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	if _, err := s.cols.Match(r.Context(), s.st, slug); err != nil {
		s.collectionsError(w, err)
		return
	}
	c, err := s.cols.Get(r.Context(), slug)
	if err != nil {
		s.collectionsError(w, err)
		return
	}
	writeJSON(w, collectionJSON(*c))
}

// POST /admin/collections/{slug}/delete
func (s *Server) handleCollectionDelete(w http.ResponseWriter, r *http.Request) {
	if s.cols == nil {
		http.Error(w, "collections unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.cols.Delete(r.Context(), r.PathValue("slug")); err != nil {
		s.collectionsError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
