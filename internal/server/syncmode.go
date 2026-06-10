package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/syncer"
)

// Desktop client sync mode: the catalog is proxied to the server,
// downloaded books are available offline, user data is written
// locally and merged with the server in the background.

// registerSyncRoutes installs the sync-mode catalog routes.
func (s *Server) registerSyncRoutes(mux *http.ServeMux) {
	// Catalog: proxy when the server is reachable.
	proxyOnly := func(w http.ResponseWriter, r *http.Request) {
		if !s.sync.Online() {
			http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
			return
		}
		s.sync.Proxy(w, r)
	}
	for _, p := range []string{
		"GET /main/getBooks/getCatalogShelves",
		"GET /main/getBooks/getShelfBooks",
		"GET /main/getBooks/getSearchStats",
		"GET /main/getBooks/getSearchAuthors",
		"GET /main/getBooks/getSearchSeries",
		"GET /main/getBooks/getSearchGenres",
		"GET /main/getBooks/getSearchAuthorBooks",
		"GET /main/getBooks/getSearchSeriesBooks",
		"GET /main/getBooks/getExternalEnrichment",
		"GET /main/getBooks/getSimilarBooks",
		"GET /Images/zip/{id}",
		"GET /Images/fb2compact/{id}",
		"GET /Images/export",
	} {
		mux.HandleFunc(p, proxyOnly)
	}

	mux.HandleFunc("GET /main/getBooks/getConfig", s.syncGetConfig)
	mux.HandleFunc("GET /main/getBooks/getHomeShelves", s.syncHomeShelves)
	mux.HandleFunc("GET /main/getBooks/getSearchTitles", s.syncSearchTitles)
	mux.HandleFunc("GET /main/getBooks/getBookForm", s.syncBookForm)
	mux.HandleFunc("GET /Images/covers/{id}", s.syncCover)
	mux.HandleFunc("GET /Images/fb2/{id}", s.syncDownload)

	// Reading: offline books locally, everything else proxied.
	mux.HandleFunc("GET /api/v1/read/{id}", s.syncReadMeta)
	mux.HandleFunc("GET /api/v1/read/{id}/chapter/{n}", s.syncReadChapter)
	mux.HandleFunc("GET /api/v1/read/{id}/img/{imgId}", s.syncReadImage)
	mux.HandleFunc("GET /api/v1/read/{id}/progress", s.handleReadProgress)
	mux.HandleFunc("POST /api/v1/read/{id}/progress", s.syncAfter(s.handleReadProgress))

	// Offline cache management
	mux.HandleFunc("GET /api/v1/offline", s.handleOfflineList)
	mux.HandleFunc("GET /api/v1/offline/{id}", s.handleOfflineStatus)
	mux.HandleFunc("POST /api/v1/offline/{id}", s.handleOfflineAdd)
	mux.HandleFunc("POST /api/v1/offline/{id}/delete", s.handleOfflineRemove)

	// Synchronization
	mux.HandleFunc("GET /api/v1/sync/info", s.handleSyncInfo)
	mux.HandleFunc("POST /api/v1/sync/now", s.handleSyncNow)
}

// syncAfter triggers background synchronization after a local write.
func (s *Server) syncAfter(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
		s.sync.RequestSync()
	}
}

// maybeSyncAfter — same, but safe for regular server mode.
func (s *Server) maybeSyncAfter(next http.HandlerFunc) http.HandlerFunc {
	if s.sync == nil {
		return next
	}
	return s.syncAfter(next)
}

// resolveBooksJSON turns a list of ids into cards: in regular mode from
// the local catalog, in sync mode from the offline cache or the server.
func (s *Server) resolveBooksJSON(r *http.Request, ids []int64) []map[string]any {
	if s.sync == nil {
		books, err := s.st.BooksByIDs(r.Context(), ids)
		if err != nil {
			return []map[string]any{}
		}
		return booksJSON(books)
	}

	out := make([]map[string]any, 0, len(ids))
	var missing []int64
	byID := map[int64]map[string]any{}
	for _, id := range ids {
		if b, err := s.sync.OfflineBook(r.Context(), id); err == nil {
			byID[id] = offlineBookJSON(*b)
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 && s.sync.Online() {
		if remote, err := s.sync.FetchBooksByIDs(r.Context(), missing); err == nil {
			for _, j := range remote {
				if id, ok := j["BookID"].(float64); ok {
					byID[int64(id)] = j
				}
			}
		}
	}
	for _, id := range ids {
		if j, ok := byID[id]; ok {
			out = append(out, j)
		}
	}
	return out
}

func (s *Server) syncGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	books, _ := s.sync.OfflineBooks(r.Context())
	writeJSON(w, map[string]any{
		"collectionName": tr(reqLang(r), "collection.offline"),
		"numberOfBooks":  len(books),
	})
}

func offlineBookJSON(b syncer.OfflineBook) map[string]any {
	return map[string]any{
		"BookID":       b.ID,
		"Title":        b.Title,
		"AuthorsNames": b.Authors,
		"SeriesTitle":  b.SeriesTitle,
		"SeqNumber":    b.SeqNumber,
		"Year":         b.Year,
		"LibRate":      b.LibRate,
	}
}

func (s *Server) offlineShelf(r *http.Request) map[string]any {
	books, err := s.sync.OfflineBooks(r.Context())
	if err != nil || len(books) == 0 {
		return nil
	}
	list := make([]map[string]any, 0, len(books))
	for _, b := range books {
		list = append(list, offlineBookJSON(b))
	}
	return map[string]any{"id": "offline", "title": tr(reqLang(r), "shelf.offline"), "books": list, "hasMore": false}
}

func (s *Server) syncHomeShelves(w http.ResponseWriter, r *http.Request) {
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	shelves := []map[string]any{}
	if shelf := s.offlineShelf(r); shelf != nil {
		shelves = append(shelves, shelf)
	}
	writeJSON(w, map[string]any{"shelves": shelves})
}

func (s *Server) syncSearchTitles(w http.ResponseWriter, r *http.Request) {
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	query := strings.ToLower(searchParam(r))
	books, _ := s.sync.OfflineBooks(r.Context())
	list := make([]map[string]any, 0)
	for _, b := range books {
		if query == "" || strings.Contains(strings.ToLower(b.Title), query) ||
			strings.Contains(strings.ToLower(b.Authors), query) {
			list = append(list, offlineBookJSON(b))
		}
	}
	writeJSON(w, map[string]any{"titlesList": list})
}

func (s *Server) syncBookForm(w http.ResponseWriter, r *http.Request) {
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	id, ok := idParam(r, "selectedItemID")
	if !ok {
		http.Error(w, "selectedItemID required", http.StatusBadRequest)
		return
	}
	b, err := s.sync.OfflineBook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resp := map[string]any{
		"bookForm": map[string]any{
			"BookID":       b.ID,
			"Title":        b.Title,
			"AuthorsNames": b.Authors,
			"LibRate":      b.LibRate,
			"BookSize":     b.Size,
			"Genres":       "",
			"Ext":          "." + b.Ext,
			"FileName":     strconv.FormatInt(b.ID, 10),
		},
		"authors":    []any{},
		"series":     []any{},
		"annotation": "",
	}
	if u := s.currentUser(r); u != nil {
		resp["userRating"] = s.users.UserRating(r.Context(), u.ID, b.ID)
		if avg, count, err := s.users.BookRating(r.Context(), b.ID); err == nil {
			resp["polkaRating"] = map[string]any{"rating": avg, "count": count}
		}
	}
	writeJSON(w, resp)
}

func (s *Server) syncCover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if data, mime, err := s.sync.OfflineCover(r.Context(), id); err == nil {
		if mime == "" {
			mime = "image/jpeg"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
		return
	}
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) syncDownload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rc, b, err := s.sync.OpenOffline(r.Context(), id)
	if err == nil {
		defer rc.Close()
		disposition := "attachment"
		if r.URL.Query().Get("inline") == "1" {
			disposition = "inline"
		}
		w.Header().Set("Content-Type", contentTypeFor(b.Ext))
		w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, b.Title+"."+b.Ext))
		w.Header().Set("Content-Length", strconv.FormatInt(b.Size, 10))
		ioCopy(w, rc)
		return
	}
	if s.sync.Online() {
		s.sync.Proxy(w, r)
		return
	}
	http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
}

// --- Reading offline books ---

// offlineText parses the local book file (with a cache of parsed books).
func (s *Server) offlineText(r *http.Request, bookID int64) (*library.FB2Text, *syncer.OfflineBook, error) {
	b, err := s.sync.OfflineBook(r.Context(), bookID)
	if err != nil {
		return nil, nil, err
	}
	if text := s.reader.get(bookID); text != nil {
		return text, b, nil
	}
	rc, _, err := s.sync.OpenOffline(r.Context(), bookID)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	var text *library.FB2Text
	switch strings.ToLower(b.Ext) {
	case "fb2":
		imgURL := func(id string) string {
			return fmt.Sprintf("/api/v1/read/%d/img/%s", bookID, id)
		}
		text, err = library.ParseFB2Text(rc, imgURL)
	case "txt":
		text, err = library.ParseTXT(rc)
	default:
		return nil, b, nil // pdf/epub: no text needed, the file is served as is
	}
	if err != nil {
		return nil, nil, err
	}
	s.reader.put(bookID, text)
	return text, b, nil
}

func (s *Server) syncReadMeta(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, b, oerr := s.offlineText(r, bookID)
	if errors.Is(oerr, syncer.ErrOffline) {
		if s.sync.Online() {
			s.sync.Proxy(w, r)
			return
		}
		http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
		return
	}
	if oerr != nil {
		s.apiError(w, oerr)
		return
	}

	format := strings.ToLower(b.Ext)
	if format == "pdf" || format == "epub" {
		writeJSON(w, map[string]any{
			"bookId": bookID, "format": format,
			"title": b.Title, "authors": b.Authors,
			"fileUrl": fmt.Sprintf("/Images/fb2/%d?inline=1", bookID),
		})
		return
	}
	lang := reqLang(r)
	chapters := make([]map[string]any, 0, len(text.Chapters))
	for i, ch := range text.Chapters {
		title := ch.Title
		if title == "" {
			if i == 0 {
				title = tr(lang, "reader.start")
			} else {
				title = tr(lang, "reader.chapter", i+1)
			}
		}
		chapters = append(chapters, map[string]any{"index": i, "title": title})
	}
	writeJSON(w, map[string]any{
		"bookId": bookID, "format": format,
		"title": b.Title, "authors": b.Authors,
		"chapters": chapters, "notes": text.Notes,
	})
}

func (s *Server) syncReadChapter(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, _, oerr := s.offlineText(r, bookID)
	if errors.Is(oerr, syncer.ErrOffline) || text == nil {
		if s.sync.Online() {
			s.sync.Proxy(w, r)
			return
		}
		http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(text.Chapters) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{
		"index": n, "total": len(text.Chapters),
		"title": text.Chapters[n].Title, "html": text.Chapters[n].HTML,
	})
}

func (s *Server) syncReadImage(w http.ResponseWriter, r *http.Request) {
	bookID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rc, b, oerr := s.sync.OpenOffline(r.Context(), bookID)
	if oerr != nil {
		if s.sync.Online() {
			s.sync.Proxy(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	if !strings.EqualFold(b.Ext, "fb2") {
		http.NotFound(w, r)
		return
	}
	data, mime, err := library.ExtractBinary(rc, r.PathValue("imgId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// --- Offline cache management ---

func (s *Server) handleOfflineList(w http.ResponseWriter, r *http.Request) {
	books, err := s.sync.OfflineBooks(r.Context())
	if err != nil {
		s.apiError(w, err)
		return
	}
	list := make([]map[string]any, 0, len(books))
	for _, b := range books {
		j := offlineBookJSON(b)
		j["size"] = b.Size
		j["ext"] = b.Ext
		list = append(list, j)
	}
	writeJSON(w, map[string]any{"books": list})
}

func offlineIDParam(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) handleOfflineStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := offlineIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, err := s.sync.OfflineBook(r.Context(), id)
	writeJSON(w, map[string]any{"offline": err == nil})
}

func (s *Server) handleOfflineAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := offlineIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.sync.Online() {
		http.Error(w, "server unavailable (offline)", http.StatusServiceUnavailable)
		return
	}
	book, err := s.sync.MakeOffline(r.Context(), id)
	if err != nil {
		s.log.Warn("offline add", "book", id, "error", err)
		http.Error(w, "download failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"offline": true, "title": book.Title, "size": book.Size})
}

func (s *Server) handleOfflineRemove(w http.ResponseWriter, r *http.Request) {
	id, ok := offlineIDParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.sync.RemoveOffline(r.Context(), id); err != nil && !errors.Is(err, syncer.ErrOffline) {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"offline": false})
}

// --- Status and manual synchronization ---

func (s *Server) handleSyncInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{
		"enabled": true,
		"server":  s.sync.Server(),
		"online":  s.sync.Online(),
	}
	if t := s.sync.LastSync(); !t.IsZero() {
		info["lastSync"] = t.UTC().Format(time.RFC3339)
	}
	writeJSON(w, info)
}

func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request) {
	if !s.sync.CheckOnline(r.Context()) {
		http.Error(w, "server unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.sync.SyncNow(r.Context()); err != nil {
		s.log.Warn("manual sync", "error", err)
		http.Error(w, "sync failed", http.StatusBadGateway)
		return
	}
	s.handleSyncInfo(w, r)
}

// ioCopy is a local alias to avoid pulling io into every handler.
func ioCopy(w http.ResponseWriter, rc interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 64<<10)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
