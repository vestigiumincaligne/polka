package server

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vestigiumincaligne/polka/internal/genres"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// OPDS 1.2 (Atom) — catalog for mobile readers: Moon+ Reader,
// KOReader, FBReader, etc. Authentication is HTTP Basic.

const (
	opdsPageSize   = 40
	opdsNavType    = "application/atom+xml;profile=opds-catalog;kind=navigation"
	opdsAcqType    = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	opdsSearchType = "application/opensearchdescription+xml"
	relAcquisition = "http://opds-spec.org/acquisition"
	relImage       = "http://opds-spec.org/image"
	relThumbnail   = "http://opds-spec.org/image/thumbnail"
)

type opdsFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Xmlns   string      `xml:"xmlns,attr"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Links   []opdsLink  `xml:"link"`
	Entries []opdsEntry `xml:"entry"`
}

type opdsLink struct {
	Rel   string `xml:"rel,attr,omitempty"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title,attr,omitempty"`
}

type opdsAuthor struct {
	Name string `xml:"name"`
}

type opdsContent struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type opdsEntry struct {
	Title   string       `xml:"title"`
	ID      string       `xml:"id"`
	Updated string       `xml:"updated"`
	Authors []opdsAuthor `xml:"author"`
	Content *opdsContent `xml:"content,omitempty"`
	Links   []opdsLink   `xml:"link"`
}

// opdsAuth — Basic authentication with a browser fallback to cookies.
// opdsEnabled — the admin can switch the OPDS catalog off.
func (s *Server) opdsEnabled(r *http.Request) bool {
	return s.users.GetSetting(r.Context(), "opds.enabled", "1") == "1"
}

func (s *Server) opdsAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.opdsEnabled(r) {
			http.NotFound(w, r)
			return
		}
		if s.authRequired() && s.currentUser(r) == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Polka", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) writeFeed(w http.ResponseWriter, feed *opdsFeed) {
	feed.Xmlns = "http://www.w3.org/2005/Atom"
	feed.Updated = time.Now().UTC().Format(time.RFC3339)
	feed.Links = append(feed.Links,
		opdsLink{Rel: "start", Href: "/opds", Type: opdsNavType},
		opdsLink{Rel: "search", Href: "/opds/opensearch", Type: opdsSearchType},
	)
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	if err := xml.NewEncoder(w).Encode(feed); err != nil {
		s.log.Warn("opds encode", "error", err)
	}
}

func opdsBookEntry(b store.Book) opdsEntry {
	now := time.Now().UTC().Format(time.RFC3339)
	entry := opdsEntry{
		Title:   b.Title,
		ID:      fmt.Sprintf("urn:polka:book:%d", b.ID),
		Updated: now,
	}
	for _, name := range strings.Split(b.AuthorNames, ", ") {
		if name != "" {
			entry.Authors = append(entry.Authors, opdsAuthor{Name: name})
		}
	}
	var desc []string
	if b.SeriesTitle != "" {
		if b.SeqNumber > 0 {
			desc = append(desc, fmt.Sprintf("%s, №%d", b.SeriesTitle, b.SeqNumber))
		} else {
			desc = append(desc, b.SeriesTitle)
		}
	}
	if b.Year > 0 {
		desc = append(desc, strconv.Itoa(b.Year))
	}
	if len(desc) > 0 {
		entry.Content = &opdsContent{Type: "text", Text: strings.Join(desc, " · ")}
	}

	id := strconv.FormatInt(b.ID, 10)
	switch strings.ToLower(b.Ext) {
	case "fb2":
		entry.Links = append(entry.Links,
			opdsLink{Rel: relAcquisition, Href: "/Images/zip/" + id, Type: "application/fb2+zip"},
			opdsLink{Rel: relAcquisition, Href: "/Images/fb2/" + id, Type: "application/x-fictionbook+xml"},
		)
	case "epub":
		entry.Links = append(entry.Links,
			opdsLink{Rel: relAcquisition, Href: "/Images/fb2/" + id, Type: "application/epub+zip"})
	case "pdf":
		entry.Links = append(entry.Links,
			opdsLink{Rel: relAcquisition, Href: "/Images/fb2/" + id, Type: "application/pdf"})
	default:
		entry.Links = append(entry.Links,
			opdsLink{Rel: relAcquisition, Href: "/Images/fb2/" + id, Type: "application/octet-stream"})
	}
	entry.Links = append(entry.Links,
		opdsLink{Rel: relImage, Href: "/Images/covers/" + id, Type: "image/jpeg"},
		opdsLink{Rel: relThumbnail, Href: "/Images/covers/" + id, Type: "image/jpeg"},
	)
	return entry
}

// acquisitionFeed builds a paginated book-list feed.
func (s *Server) acquisitionFeed(id, title, baseHref string, page int, books []store.Book, hasMore bool) *opdsFeed {
	feed := &opdsFeed{ID: id, Title: title}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: pageHref(baseHref, page), Type: opdsAcqType})
	if hasMore {
		feed.Links = append(feed.Links, opdsLink{Rel: "next", Href: pageHref(baseHref, page+1), Type: opdsAcqType})
	}
	if page > 0 {
		feed.Links = append(feed.Links, opdsLink{Rel: "previous", Href: pageHref(baseHref, page-1), Type: opdsAcqType})
	}
	for _, b := range books {
		feed.Entries = append(feed.Entries, opdsBookEntry(b))
	}
	return feed
}

func pageHref(base string, page int) string {
	if page <= 0 {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%spage=%d", base, sep, page)
}

func opdsPage(r *http.Request) int {
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		return p
	}
	return 0
}

// --- Handlers ---

// GET /opds — root navigation feed.
func (s *Server) handleOpdsRoot(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	name := s.st.GetMeta(r.Context(), "collection_name", "Полка")

	feed := &opdsFeed{ID: "urn:polka:root", Title: name}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: "/opds", Type: opdsNavType})

	lang := reqLang(r)
	entries := []struct {
		id, title, sub, href, typ string
	}{
		{"new", tr(lang, "opds.new"), tr(lang, "opds.new.sub"), "/opds/new", opdsAcqType},
		{"authors", tr(lang, "opds.authors"), tr(lang, "opds.authors.sub"), "/opds/authors", opdsNavType},
		{"series", tr(lang, "opds.series"), tr(lang, "opds.series.sub"), "/opds/series", opdsNavType},
		{"genres", tr(lang, "opds.genres"), tr(lang, "opds.genres.sub"), "/opds/genres", opdsNavType},
	}
	if s.currentUser(r) != nil {
		entries = append(entries, struct {
			id, title, sub, href, typ string
		}{"reading", tr(lang, "opds.reading"), tr(lang, "opds.reading.sub"), "/opds/reading", opdsAcqType})
	}
	for _, e := range entries {
		feed.Entries = append(feed.Entries, opdsEntry{
			Title:   e.title,
			ID:      "urn:polka:nav:" + e.id,
			Updated: now,
			Content: &opdsContent{Type: "text", Text: e.sub},
			Links:   []opdsLink{{Href: e.href, Type: e.typ}},
		})
	}
	s.writeFeed(w, feed)
}

// GET /opds/opensearch — search description.
func (s *Server) handleOpdsOpenSearch(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", opdsSearchType)
	fmt.Fprint(w, xml.Header, `<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Полка</ShortName>
  <Description>Поиск книг по названию, автору и серии</Description>
  <InputEncoding>UTF-8</InputEncoding>
  <OutputEncoding>UTF-8</OutputEncoding>
  <Url type="application/atom+xml;profile=opds-catalog;kind=acquisition" template="/opds/search?q={searchTerms}"/>
</OpenSearchDescription>`)
}

// GET /opds/new — new arrivals with pagination.
func (s *Server) handleOpdsNew(w http.ResponseWriter, r *http.Request) {
	page := opdsPage(r)
	books, _, err := s.st.ShelfBooks(r.Context(), "newest", opdsPageSize+1, page*opdsPageSize)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(books) > opdsPageSize
	if hasMore {
		books = books[:opdsPageSize]
	}
	s.writeFeed(w, s.acquisitionFeed("urn:polka:new", tr(reqLang(r), "opds.new"), "/opds/new", page, books, hasMore))
}

// GET /opds/genres — genre navigation.
func (s *Server) handleOpdsGenres(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.GenresWithCounts(r.Context())
	if err != nil {
		s.apiError(w, err)
		return
	}
	lang := reqLang(r)
	type namedGenre struct {
		store.GenreCount
		Name string
	}
	named := make([]namedGenre, 0, len(list))
	for _, g := range list {
		named = append(named, namedGenre{g, genres.NameLang(g.Code, lang)})
	}
	// Genres with a Russian name come first, raw codes go to the end.
	sort.Slice(named, func(i, j int) bool {
		ti, tj := named[i].Name != named[i].Code, named[j].Name != named[j].Code
		if ti != tj {
			return ti
		}
		return named[i].Name < named[j].Name
	})

	now := time.Now().UTC().Format(time.RFC3339)
	feed := &opdsFeed{ID: "urn:polka:genres", Title: tr(lang, "opds.genres")}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: "/opds/genres", Type: opdsNavType})
	for _, g := range named {
		feed.Entries = append(feed.Entries, opdsEntry{
			Title:   g.Name,
			ID:      "urn:polka:genre:" + g.Code,
			Updated: now,
			Content: &opdsContent{Type: "text", Text: tr(lang, "opds.books", g.Books)},
			Links:   []opdsLink{{Href: "/opds/genre/" + g.Code, Type: opdsAcqType}},
		})
	}
	s.writeFeed(w, feed)
}

// GET /opds/genre/{code} — books of a genre.
func (s *Server) handleOpdsGenre(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	page := opdsPage(r)
	books, _, err := s.st.ShelfBooks(r.Context(), "genre_"+code, opdsPageSize+1, page*opdsPageSize)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(books) > opdsPageSize
	if hasMore {
		books = books[:opdsPageSize]
	}
	s.writeFeed(w, s.acquisitionFeed("urn:polka:genre:"+code, genres.NameLang(code, reqLang(r)), "/opds/genre/"+code, page, books, hasMore))
}

// GET /opds/search?q= — search by title/author/series.
func (s *Server) handleOpdsSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("q"), "+", " "))
	page := opdsPage(r)
	books, err := s.st.SearchBooks(r.Context(), query, opdsPageSize+1, page*opdsPageSize)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(books) > opdsPageSize
	if hasMore {
		books = books[:opdsPageSize]
	}
	base := "/opds/search?q=" + strings.ReplaceAll(query, " ", "+")
	s.writeFeed(w, s.acquisitionFeed("urn:polka:search", tr(reqLang(r), "opds.search", query), base, page, books, hasMore))
}

// GET /opds/reading — books with a reading position (login required).
func (s *Server) handleOpdsReading(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Polka", charset="UTF-8"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	progress, err := s.users.ListProgress(r.Context(), u.ID, opdsPageSize)
	if err != nil {
		s.apiError(w, err)
		return
	}
	ids := make([]int64, 0, len(progress))
	for _, p := range progress {
		ids = append(ids, p.BookID)
	}
	books, err := s.st.BooksByIDs(r.Context(), ids)
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.writeFeed(w, s.acquisitionFeed("urn:polka:reading", tr(reqLang(r), "opds.reading"), "/opds/reading", 0, books, false))
}

// --- Author and series navigation (alphabetical browse) ---

// opdsLetters — the A–Z buckets for browsing authors/series.
var opdsLetters = []string{
	"А", "Б", "В", "Г", "Д", "Е", "Ж", "З", "И", "К", "Л", "М", "Н", "О",
	"П", "Р", "С", "Т", "У", "Ф", "Х", "Ц", "Ч", "Ш", "Щ", "Э", "Ю", "Я",
	"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N",
	"O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
}

// letterRange returns the [lo, hi) bounds for a single-letter bucket.
func letterRange(letter string) (lo, hi string, ok bool) {
	rs := []rune(letter)
	if len(rs) != 1 {
		return "", "", false
	}
	return string(rs[0]), string(rs[0] + 1), true
}

// opdsLetterFeed builds the A–Z navigation feed shared by authors/series.
func (s *Server) opdsLetterFeed(id, title, base string) *opdsFeed {
	now := time.Now().UTC().Format(time.RFC3339)
	feed := &opdsFeed{ID: id, Title: title}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: base, Type: opdsNavType})
	for _, l := range opdsLetters {
		feed.Entries = append(feed.Entries, opdsEntry{
			Title:   l,
			ID:      id + ":" + l,
			Updated: now,
			Links:   []opdsLink{{Href: base + "/" + url.PathEscape(l), Type: opdsNavType}},
		})
	}
	return feed
}

// GET /opds/authors — letters.
func (s *Server) handleOpdsAuthors(w http.ResponseWriter, r *http.Request) {
	s.writeFeed(w, s.opdsLetterFeed("urn:polka:authors", tr(reqLang(r), "opds.authors"), "/opds/authors"))
}

// GET /opds/series — letters.
func (s *Server) handleOpdsSeriesList(w http.ResponseWriter, r *http.Request) {
	s.writeFeed(w, s.opdsLetterFeed("urn:polka:series", tr(reqLang(r), "opds.series"), "/opds/series"))
}

const opdsBrowsePage = 60

// GET /opds/authors/{letter} — authors for a letter, paginated.
func (s *Server) handleOpdsAuthorLetter(w http.ResponseWriter, r *http.Request) {
	letter := r.PathValue("letter")
	lo, hi, ok := letterRange(letter)
	if !ok {
		http.NotFound(w, r)
		return
	}
	lang := reqLang(r)
	page := opdsPage(r)
	authors, err := s.st.AuthorsByPrefix(r.Context(), lo, hi, opdsBrowsePage+1, page*opdsBrowsePage)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(authors) > opdsBrowsePage
	if hasMore {
		authors = authors[:opdsBrowsePage]
	}
	base := "/opds/authors/" + url.PathEscape(letter)
	now := time.Now().UTC().Format(time.RFC3339)
	feed := &opdsFeed{ID: "urn:polka:authors:" + letter, Title: letter}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: pageHref(base, page), Type: opdsNavType})
	if hasMore {
		feed.Links = append(feed.Links, opdsLink{Rel: "next", Href: pageHref(base, page+1), Type: opdsNavType})
	}
	for _, a := range authors {
		feed.Entries = append(feed.Entries, opdsEntry{
			Title:   a.Name,
			ID:      fmt.Sprintf("urn:polka:author:%d", a.ID),
			Updated: now,
			Content: &opdsContent{Type: "text", Text: tr(lang, "opds.books", a.Books)},
			Links:   []opdsLink{{Href: fmt.Sprintf("/opds/author/%d", a.ID), Type: opdsAcqType}},
		})
	}
	s.writeFeed(w, feed)
}

// GET /opds/author/{id} — books by an author.
func (s *Server) handleOpdsAuthor(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	books, name, err := s.st.AuthorBooks(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.writeFeed(w, s.acquisitionFeed(fmt.Sprintf("urn:polka:author:%d", id), name, fmt.Sprintf("/opds/author/%d", id), 0, books, false))
}

// GET /opds/series/{letter} — series for a letter.
func (s *Server) handleOpdsSeriesLetter(w http.ResponseWriter, r *http.Request) {
	letter := r.PathValue("letter")
	lo, hi, ok := letterRange(letter)
	if !ok {
		http.NotFound(w, r)
		return
	}
	lang := reqLang(r)
	page := opdsPage(r)
	series, err := s.st.SeriesByPrefix(r.Context(), lo, hi, opdsBrowsePage+1, page*opdsBrowsePage)
	if err != nil {
		s.apiError(w, err)
		return
	}
	hasMore := len(series) > opdsBrowsePage
	if hasMore {
		series = series[:opdsBrowsePage]
	}
	base := "/opds/series/" + url.PathEscape(letter)
	now := time.Now().UTC().Format(time.RFC3339)
	feed := &opdsFeed{ID: "urn:polka:series:" + letter, Title: letter}
	feed.Links = append(feed.Links, opdsLink{Rel: "self", Href: pageHref(base, page), Type: opdsNavType})
	if hasMore {
		feed.Links = append(feed.Links, opdsLink{Rel: "next", Href: pageHref(base, page+1), Type: opdsNavType})
	}
	for _, se := range series {
		feed.Entries = append(feed.Entries, opdsEntry{
			Title:   se.Title,
			ID:      fmt.Sprintf("urn:polka:seriesbooks:%d", se.ID),
			Updated: now,
			Content: &opdsContent{Type: "text", Text: tr(lang, "opds.books", se.Books)},
			Links:   []opdsLink{{Href: fmt.Sprintf("/opds/series/id/%d", se.ID), Type: opdsAcqType}},
		})
	}
	s.writeFeed(w, feed)
}

// GET /opds/series/id/{id} — books in a series.
func (s *Server) handleOpdsSeriesOne(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	books, title, err := s.st.SeriesBooks(r.Context(), id)
	if err != nil {
		s.apiError(w, err)
		return
	}
	s.writeFeed(w, s.acquisitionFeed(fmt.Sprintf("urn:polka:seriesbooks:%d", id), title, fmt.Sprintf("/opds/series/id/%d", id), 0, books, false))
}
