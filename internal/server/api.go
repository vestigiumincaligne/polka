package server

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/genres"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// bookJSON converts a book to the format expected by the frontend.
func bookJSON(b store.Book) map[string]any {
	return map[string]any{
		"BookID":       b.ID,
		"Title":        b.Title,
		"AuthorsNames": b.AuthorNames,
		"SeriesTitle":  b.SeriesTitle,
		"SeqNumber":    b.SeqNumber,
		"Year":         b.Year,
		"LibRate":      b.LibRate,
	}
}

func booksJSON(books []store.Book) []map[string]any {
	out := make([]map[string]any, 0, len(books))
	for _, b := range books {
		out = append(out, bookJSON(b))
	}
	return out
}

func shelvesJSON(shelves []store.Shelf) []map[string]any {
	out := make([]map[string]any, 0, len(shelves))
	for _, sh := range shelves {
		out = append(out, map[string]any{
			"id":      sh.ID,
			"title":   sh.Title,
			"books":   booksJSON(sh.Books),
			"hasMore": sh.HasMore,
		})
	}
	return out
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return id, err == nil && id > 0
}

// searchParam: URL parameters encode a space as '+'.
func searchParam(r *http.Request) string {
	return strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("search"), "+", " "))
}

func (s *Server) apiError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, library.ErrNoFile) {
		http.NotFound(w, nil)
		return
	}
	s.log.Error("api", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// --- main/getBooks/* ---

func (s *Server) handleGetHomeShelves(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 12)
	shelves, err := s.st.HomeShelves(r.Context(), limit)
	if err != nil {
		s.apiError(w, err)
		return
	}
	out := shelvesJSON(shelves)

	// Personal shelves on top: "Reading now", "Want to read",
	// then recommendations ("Continue series", "For you").
	if u := s.currentUser(r); u != nil {
		lang := reqLang(r)
		var personal []map[string]any
		if reading := s.readingShelf(r, u.ID, limit); len(reading) > 0 {
			personal = append(personal, map[string]any{
				"id": "reading", "title": tr(lang, "shelf.reading"), "books": reading, "hasMore": false,
			})
		}
		if wishlist := s.wishlistShelf(r, u.ID, limit); wishlist != nil {
			wishlist["title"] = tr(lang, "shelf.wishlist")
			personal = append(personal, wishlist)
		}
		personal = append(personal, s.recShelves(r, u.ID, limit)...)
		out = append(personal, out...)
	}
	writeJSON(w, map[string]any{"shelves": out})
}

// wishlistShelf is the "Want to read" shelf, if it has any books.
func (s *Server) wishlistShelf(r *http.Request, userID int64, limit int) map[string]any {
	wl, err := s.users.Wishlist(r.Context(), userID)
	if err != nil {
		return nil
	}
	ids, err := s.users.ListBookIDs(r.Context(), userID, wl.ID, limit+1, 0)
	if err != nil || len(ids) == 0 {
		return nil
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	books, err := s.st.BooksByIDs(r.Context(), ids)
	if err != nil || len(books) == 0 {
		return nil
	}
	return map[string]any{
		"id": "wishlist", "title": wl.Name, "books": booksJSON(books), "hasMore": hasMore,
	}
}

// readingShelf returns books from reading progress with the fraction read.
func (s *Server) readingShelf(r *http.Request, userID int64, limit int) []map[string]any {
	progress, err := s.users.ListProgress(r.Context(), userID, limit)
	if err != nil || len(progress) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(progress))
	overall := make(map[int64]float64, len(progress))
	for _, p := range progress {
		ids = append(ids, p.BookID)
		overall[p.BookID] = p.Overall
	}
	books, err := s.st.BooksByIDs(r.Context(), ids)
	if err != nil {
		s.log.Warn("reading shelf", "error", err)
		return nil
	}
	out := make([]map[string]any, 0, len(books))
	for _, b := range books {
		j := bookJSON(b)
		j["ReadingProgress"] = overall[b.ID]
		out = append(out, j)
	}
	return out
}

func (s *Server) handleGetCatalogShelves(w http.ResponseWriter, r *http.Request) {
	shelves, err := s.st.CatalogShelves(r.Context(), intParam(r, "count", 6), intParam(r, "limit", 12))
	if err != nil {
		s.apiError(w, err)
		return
	}
	lang := reqLang(r)
	for i := range shelves {
		shelves[i].Title = genres.NameLang(strings.TrimPrefix(shelves[i].Title, "genre_"), lang)
	}
	writeJSON(w, map[string]any{"shelves": shelvesJSON(shelves)})
}

func (s *Server) handleGetShelfBooks(w http.ResponseWriter, r *http.Request) {
	shelfID := r.URL.Query().Get("shelfId")
	limit := intParam(r, "limit", 60)
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	books, title, err := s.st.ShelfBooks(r.Context(), shelfID, limit+1, offset)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(books) > limit
	if hasMore {
		books = books[:limit]
	}
	if strings.HasPrefix(shelfID, "genre_") {
		title = genres.NameLang(strings.TrimPrefix(shelfID, "genre_"), reqLang(r))
	}
	writeJSON(w, map[string]any{
		"titlesList": booksJSON(books),
		"title":      title,
		"hasMore":    hasMore,
		"nextOffset": offset + len(books),
	})
}

// GET /main/getBooks/getBooksByIds?ids=1,2,3 — book cards by id
// (used by desktop clients for books from lists).
func (s *Server) handleGetBooksByIDs(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > 500 {
		http.Error(w, "ids: from 1 to 500 book ids", http.StatusBadRequest)
		return
	}
	books, err := s.st.BooksByIDs(r.Context(), ids)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"titlesList": booksJSON(books)})
}

func (s *Server) handleGetSearchStats(w http.ResponseWriter, r *http.Request) {
	query := searchParam(r)
	if store.LooksLikeISBN(query) {
		writeJSON(w, map[string]any{"searchStats": map[string]int{
			"bookTitles": len(s.isbnBooks(r, query)), "authors": 0, "bookSeries": 0, "genres": 0,
		}})
		return
	}
	stats, err := s.st.SearchStats(r.Context(), query)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"searchStats": map[string]int{
		"bookTitles": stats.BookTitles,
		"authors":    stats.Authors,
		"bookSeries": stats.BookSeries,
		"genres":     len(s.searchGenres(r, query)),
	}})
}

func (s *Server) handleGetSearchTitles(w http.ResponseWriter, r *http.Request) {
	query := searchParam(r)
	if store.LooksLikeISBN(query) {
		writeJSON(w, map[string]any{"titlesList": booksJSON(s.isbnBooks(r, query))})
		return
	}
	books, err := s.st.SearchTitles(r.Context(), query, 250)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"titlesList": booksJSON(books)})
}

func (s *Server) handleGetSearchAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := s.st.SearchAuthors(r.Context(), searchParam(r), 250)
	if err != nil {
		s.apiError(w, err)
		return
	}
	list := make([]map[string]any, 0, len(authors))
	for _, a := range authors {
		list = append(list, map[string]any{"AuthorID": a.ID, "Authors": a.Name, "Books": a.Books})
	}
	writeJSON(w, map[string]any{"authorsList": list})
}

func (s *Server) handleGetSearchSeries(w http.ResponseWriter, r *http.Request) {
	series, err := s.st.SearchSeries(r.Context(), searchParam(r), 250)
	if err != nil {
		s.apiError(w, err)
		return
	}
	list := make([]map[string]any, 0, len(series))
	for _, e := range series {
		list = append(list, map[string]any{"SeriesID": e.ID, "SeriesTitle": e.Title, "Books": e.Books})
	}
	writeJSON(w, map[string]any{"seriesList": list})
}

func (s *Server) handleGetAuthorBooks(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "selectedItemID")
	if !ok {
		http.Error(w, "selectedItemID required", http.StatusBadRequest)
		return
	}
	books, name, err := s.st.AuthorBooks(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"titlesList": booksJSON(books), "title": name})
}

func (s *Server) handleGetSeriesBooks(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "selectedItemID")
	if !ok {
		http.Error(w, "selectedItemID required", http.StatusBadRequest)
		return
	}
	books, title, err := s.st.SeriesBooks(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	writeJSON(w, map[string]any{"titlesList": booksJSON(books), "title": title})
}

func (s *Server) handleGetBookForm(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "selectedItemID")
	if !ok {
		http.Error(w, "selectedItemID required", http.StatusBadRequest)
		return
	}
	d, err := s.st.BookDetails(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}

	genreNames := make([]string, 0, len(d.Genres))
	for _, code := range d.Genres {
		genreNames = append(genreNames, genres.NameLang(code, reqLang(r)))
	}

	authors := make([]map[string]any, 0, len(d.Authors))
	for _, a := range d.Authors {
		authors = append(authors, map[string]any{
			"LastName": a.Last, "FirstName": a.First, "MiddleName": a.Middle,
		})
	}
	seriesList := make([]map[string]any, 0, len(d.Series))
	for _, sr := range d.Series {
		seriesList = append(seriesList, map[string]any{
			"SeriesID": sr.ID, "SeriesTitle": sr.Title, "SeqNumber": sr.SeqNumber,
		})
	}

	resp := map[string]any{
		"bookForm": map[string]any{
			"BookID":       d.ID,
			"Title":        d.Title,
			"AuthorsNames": d.AuthorNames,
			"LibRate":      d.LibRate,
			"BookSize":     d.Size,
			"Genres":       strings.Join(genreNames, ", "),
			"Ext":          "." + d.Ext,
			"FileName":     d.File,
		},
		"authors":    authors,
		"series":     seriesList,
		"annotation": "",
	}

	// Polka's internal ratings: overall and the current user's.
	if avg, count, err := s.users.BookRating(r.Context(), d.ID); err == nil {
		resp["polkaRating"] = map[string]any{"rating": avg, "count": count}
	}
	if u := s.currentUser(r); u != nil {
		resp["userRating"] = s.users.UserRating(r.Context(), u.ID, d.ID)
		if listIDs, err := s.users.BookListIDs(r.Context(), u.ID, d.ID); err == nil {
			resp["bookListIds"] = listIDs
		}
	}

	if s.lib != nil {
		if meta, err := s.lib.Meta(d.Folder, d.File, d.Ext); err == nil {
			resp["annotation"] = meta.AnnotationHTML
			resp["publisher"] = meta.Publisher
			resp["city"] = meta.City
			resp["year"] = meta.Year
			resp["isbn"] = meta.ISBN
			// Lazy indexing: the ISBN from the file becomes searchable.
			s.st.SetBookISBN(r.Context(), d.ID, meta.ISBN)
		} else if !errors.Is(err, library.ErrNoFile) {
			s.log.Warn("fb2 meta", "book", d.ID, "error", err)
		}
	}
	writeJSON(w, resp)
}

// --- Images/* ---

func (s *Server) bookFileOr404(w http.ResponseWriter, r *http.Request) *store.BookFile {
	if s.lib == nil {
		http.Error(w, "library dir is not configured", http.StatusNotFound)
		return nil
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	f, err := s.st.BookFile(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return nil
	}
	return f
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	f := s.bookFileOr404(w, r)
	if f == nil {
		return
	}
	data, mime, err := s.lib.Cover(f.ID, f.Folder, f.File, f.Ext)
	if err != nil {
		s.log.Info("no cover", "book", f.ID, "folder", f.Folder, "file", f.File, "error", err)
		s.apiError(w, err)
		return
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleBookDownload(w http.ResponseWriter, r *http.Request) {
	f := s.bookFileOr404(w, r)
	if f == nil {
		return
	}
	rc, size, err := s.lib.Open(f.Folder, f.File, f.Ext)
	if err != nil {
		s.apiError(w, err)
		return
	}
	defer rc.Close()

	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disposition = "inline" // view in browser (PDF etc.)
	}
	w.Header().Set("Content-Type", contentTypeFor(f.Ext))
	w.Header().Set("Content-Disposition", disposition+`; filename="`+f.File+`.`+f.Ext+`"`)
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	io.Copy(w, rc)
}

func (s *Server) handleBookZip(w http.ResponseWriter, r *http.Request) {
	f := s.bookFileOr404(w, r)
	if f == nil {
		return
	}
	rc, _, err := s.lib.Open(f.Folder, f.File, f.Ext)
	if err != nil {
		s.apiError(w, err)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.File+`.`+f.Ext+`.zip"`)

	zw := zip.NewWriter(w)
	entry, err := zw.Create(f.File + "." + f.Ext)
	if err == nil {
		_, err = io.Copy(entry, rc)
	}
	if err != nil {
		s.log.Warn("zip stream", "book", f.ID, "error", err)
		return
	}
	zw.Close()
}

func (s *Server) handleBookCompact(w http.ResponseWriter, r *http.Request) {
	f := s.bookFileOr404(w, r)
	if f == nil {
		return
	}
	if !strings.EqualFold(f.Ext, "fb2") {
		http.NotFound(w, r)
		return
	}
	rc, _, err := s.lib.Open(f.Folder, f.File, f.Ext)
	if err != nil {
		s.apiError(w, err)
		return
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		s.apiError(w, err)
		return
	}
	compact := library.StripBinaries(data)
	w.Header().Set("Content-Type", contentTypeFor("fb2"))
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.File+`.compact.fb2"`)
	w.Write(compact)
}

func contentTypeFor(ext string) string {
	switch strings.ToLower(ext) {
	case "fb2":
		return "application/x-fictionbook+xml; charset=utf-8"
	case "epub":
		return "application/epub+zip"
	case "pdf":
		return "application/pdf"
	case "djvu":
		return "image/vnd.djvu"
	case "txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
