package sources

import (
	"context"
	"html"
	"regexp"
	"strings"

	"github.com/vestigiumincaligne/polka/internal/collections"
)

// Forbes scrapes articles tagged "Подборки книг" on forbes.ru. Each article
// ("Seven books on managing stress") becomes a collection; books in the
// articles are headed as «Title», Author.
type Forbes struct {
	base string // site root; overridden in tests
}

const forbesBase = "https://www.forbes.ru"

func NewForbes(base string) *Forbes {
	if base == "" {
		base = forbesBase
	}
	return &Forbes{base: strings.TrimRight(base, "/")}
}

func (f *Forbes) ID() string { return "forbes" }

// forbesArticleRe matches an article link: /section/123456-slug (without /page/…).
var (
	forbesArticleRe = regexp.MustCompile(`"(/[a-z][a-z-]*/(\d{5,7})-[a-z0-9-]+)"`)
	forbesTitleRe   = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	forbesH2Re      = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	forbesTagRe     = regexp.MustCompile(`<[^>]+>`)
	// «Title», Author is a book heading inside an article; anthologies
	// may have no author: «Верю / не верю».
	forbesBookRe = regexp.MustCompile(`^«(.+?)»\s*(?:[,—–-]\s*(.+))?$`)
)

// Fetch walks the tag page and parses new articles. An error in one
// article does not stop the others; an error on the tag page does.
func (f *Forbes) Fetch(ctx context.Context, known map[string]bool) ([]*collections.File, error) {
	page, err := get(ctx, f.base+"/tegi/podborki-knig")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []*collections.File
	for _, m := range forbesArticleRe.FindAllStringSubmatch(string(page), -1) {
		path, id := m[1], m[2]
		if strings.HasPrefix(path, "/page/") || seen[id] {
			continue
		}
		seen[id] = true
		slug := "forbes-" + id
		if known[slug] {
			continue
		}
		body, err := get(ctx, f.base+path)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			continue
		}
		file := ParseForbesArticle(string(body))
		if file == nil {
			continue
		}
		file.Slug = slug
		file.URL = forbesBase + path
		out = append(out, file)
	}
	return out, nil
}

// ParseForbesArticle extracts a collection from article HTML: the h1 heading and
// books from h2 elements of the form «Title», Author. Fewer than two books is not a collection.
func ParseForbesArticle(page string) *collections.File {
	title := ""
	if m := forbesTitleRe.FindStringSubmatch(page); m != nil {
		title = cleanText(m[1])
	}
	if title == "" {
		return nil
	}
	var items []collections.FileItem
	for _, m := range forbesH2Re.FindAllStringSubmatch(page, -1) {
		h := cleanText(m[1])
		bm := forbesBookRe.FindStringSubmatch(h)
		if bm == nil {
			continue
		}
		title := strings.TrimSpace(bm[1])
		author := strings.TrimSpace(bm[2])
		if author == "" && len([]rune(title)) < 6 {
			continue // a short title without an author would match anything
		}
		// «Эмили Нагоски и Амелия Нагоски» → «Эмили Нагоски, Амелия Нагоски»
		author = strings.ReplaceAll(author, " и ", ", ")
		items = append(items, collections.FileItem{Title: title, Author: author})
	}
	if len(items) < 2 {
		return nil
	}
	return &collections.File{
		Title:  title,
		Source: "Forbes",
		Items:  items,
	}
}

func cleanText(s string) string {
	s = forbesTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	return strings.Join(strings.Fields(s), " ")
}
