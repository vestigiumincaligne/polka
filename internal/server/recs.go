package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/enrich"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// Recommendations: personal shelves on the home page and "similar books"
// on the book card (local scoring + external sources).

// userSeeds gathers the user's signals: liked books (rating >= 4),
// lists, currently reading; plus everything familiar — for exclusion.
func (s *Server) userSeeds(r *http.Request, userID int64) (seeds, exclude []int64) {
	ctx := r.Context()
	liked, _ := s.users.RatedBookIDs(ctx, userID, 4)
	listed, _ := s.users.AllListBookIDs(ctx, userID)
	var reading []int64
	if progress, err := s.users.ListProgress(ctx, userID, 100); err == nil {
		for _, p := range progress {
			reading = append(reading, p.BookID)
		}
	}
	rated, _ := s.users.RatedBookIDs(ctx, userID, 0)

	seen := map[int64]bool{}
	add := func(dst *[]int64, ids []int64, cap int) {
		for _, id := range ids {
			if len(*dst) >= cap {
				return
			}
			if !seen[id] {
				seen[id] = true
				*dst = append(*dst, id)
			}
		}
	}
	// Limit seeds to the 60 most recent: SQL with IN lists must stay lightweight.
	add(&seeds, liked, 60)
	add(&seeds, listed, 60)
	add(&seeds, reading, 60)

	excludeSeen := map[int64]bool{}
	for _, ids := range [][]int64{rated, listed, reading} {
		for _, id := range ids {
			if !excludeSeen[id] {
				excludeSeen[id] = true
				exclude = append(exclude, id)
			}
		}
	}
	if len(exclude) > 400 {
		exclude = exclude[:400]
	}
	return seeds, exclude
}

// recShelves builds the personal recommendation shelves.
func (s *Server) recShelves(r *http.Request, userID int64, limit int) []map[string]any {
	seeds, exclude := s.userSeeds(r, userID)
	if len(seeds) == 0 {
		return nil
	}
	lang := reqLang(r)
	var shelves []map[string]any

	if next, err := s.st.SeriesContinuations(r.Context(), seeds, exclude, limit); err == nil && len(next) > 0 {
		// Collections contain duplicate editions — collapse by title.
		seen := map[string]bool{}
		deduped := next[:0]
		for _, b := range next {
			key := strings.ToLower(b.Title)
			if !seen[key] {
				seen[key] = true
				deduped = append(deduped, b)
			}
		}
		shelves = append(shelves, map[string]any{
			"id": "series_next", "title": tr(lang, "shelf.series_next"), "books": booksJSON(deduped), "hasMore": false,
		})
	}
	if recs, err := s.st.RecommendForUser(r.Context(), seeds, exclude, limit); err == nil && len(recs) > 0 {
		shelves = append(shelves, map[string]any{
			"id": "for_you", "title": tr(lang, "shelf.for_you"), "books": booksJSON(recs), "hasMore": false,
		})
	}
	return shelves
}

// similarConfig reads the similar-books source settings.
func (s *Server) similarConfig(r *http.Request) enrich.SimilarConfig {
	ctx := r.Context()
	return enrich.SimilarConfig{
		FantLab:      s.users.GetSetting(ctx, "enrich.similar_fantlab", "1") == "1",
		TasteDive:    s.users.GetSetting(ctx, "enrich.similar_tastedive", "0") == "1",
		TasteDiveKey: s.users.GetSetting(ctx, "tastedive_key", ""),
	}
}

// GET /main/getBooks/getSimilarBooks?bookId&title&author
func (s *Server) handleGetSimilarBooks(w http.ResponseWriter, r *http.Request) {
	bookID, ok := idParam(r, "bookId")
	if !ok {
		http.Error(w, "bookId required", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("title"), "+", " "))
	author := strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("author"), "+", " "))

	local, err := s.st.SimilarBooks(r.Context(), bookID, 24)
	if err != nil {
		s.apiError(w, err)
		return
	}
	// Diversity: don't let one author take over the whole similar shelf.
	perAuthor := map[string]int{}
	diverse := local[:0]
	for _, b := range local {
		key := strings.ToLower(b.AuthorNames)
		if perAuthor[key] >= 5 {
			continue
		}
		perAuthor[key]++
		diverse = append(diverse, b)
		if len(diverse) >= 12 {
			break
		}
	}
	local = diverse
	seen := map[int64]bool{bookID: true}
	for _, b := range local {
		seen[b.ID] = true
	}

	// External sources: ones matched to the collection become cards,
	// the rest a plain-text list.
	var externalCards []store.Book
	var externalOnly []map[string]any
	if title != "" {
		cfg := s.similarConfig(r)
		for _, sim := range s.enrich.Similar(r.Context(), "sim:"+strconv.FormatInt(bookID, 10), title, author, cfg) {
			if b, err := s.st.MatchBook(r.Context(), sim.Title, sim.Author); err == nil {
				if !seen[b.ID] {
					seen[b.ID] = true
					externalCards = append(externalCards, *b)
				}
				continue
			}
			externalOnly = append(externalOnly, map[string]any{
				"title": sim.Title, "author": sim.Author, "source": sim.Source,
			})
		}
	}

	books := booksJSON(local)
	books = append(books, booksJSON(externalCards)...)
	if len(books) > 18 {
		books = books[:18]
	}
	writeJSON(w, map[string]any{"similar": books, "external": externalOnly})
}
