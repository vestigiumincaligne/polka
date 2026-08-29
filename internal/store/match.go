package store

import (
	"context"
	"strings"
)

// Kinds of match between a library book and an external entry (collections).
const (
	MatchISBN  = "isbn"  // by ISBN
	MatchExact = "exact" // title matched entirely + author
	MatchFuzzy = "fuzzy" // title matched by prefix (subtitle, volume) + author
)

// MatchBookExt looks up a book by an external list entry: first by ISBN,
// then by FTS over the title words (and the author's last name, if any),
// picking the best candidate: an exact normalized title match is preferred
// over a prefix match; on a tie, the closer length and the higher rating
// win. The subtitle ("Title: …", "Title (novel)") is dropped in a
// second attempt. Returns the match kind or ErrNotFound.
func (s *Store) MatchBookExt(ctx context.Context, title, author, isbn string) (*Book, string, error) {
	if n := NormalizeISBN(isbn); n != "" {
		if books, err := s.SearchByISBN(ctx, n); err == nil && len(books) > 0 {
			return &books[0], MatchISBN, nil
		}
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, "", ErrNotFound
	}
	lastNames := authorLastNames(author)
	if b, kind := s.matchByFTS(ctx, title, lastNames); b != nil {
		return b, kind, nil
	}
	if short := stripSubtitle(title); short != title {
		if b, kind := s.matchByFTS(ctx, short, lastNames); b != nil {
			return b, kind, nil
		}
	}
	return nil, "", ErrNotFound
}

// stripSubtitle cuts off the subtitle: "Title: subtitle",
// "Title (novel)", "Title. Volume 1".
func stripSubtitle(title string) string {
	for _, sep := range []string{":", " (", ". ", " — ", " - "} {
		if i := strings.Index(title, sep); i > 2 {
			title = title[:i]
		}
	}
	return strings.TrimSpace(title)
}

// authorLastNames returns last-name candidates from an author string: "First Last",
// "Last First", "Last, First", several authors separated by commas
// ("Илья Ильф, Евгений Петров"). Both ends are taken for each author.
func authorLastNames(author string) []string {
	parts := strings.Split(author, ",")
	multi := false
	for _, p := range parts {
		if len(strings.Fields(p)) > 1 {
			multi = true
		}
	}
	if len(parts) > 1 && !multi {
		// "Last, First"
		return []string{strings.TrimSpace(parts[0])}
	}
	var out []string
	for _, p := range parts {
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
		if len(fields) > 1 && fields[len(fields)-1] != fields[0] {
			out = append(out, fields[len(fields)-1])
		}
	}
	return out
}

func (s *Store) matchByFTS(ctx context.Context, title string, lastNames []string) (*Book, string) {
	want := normalizeForMatch(title)
	if want == "" {
		return nil, ""
	}
	titleQ := ftsQuery(title, "title")
	if titleQ == "" {
		return nil, ""
	}
	var queries []string
	for _, ln := range lastNames {
		if aq := ftsQuery(ln, "authors"); aq != "" {
			queries = append(queries, titleQ+" AND "+aq)
		}
	}
	if len(queries) == 0 {
		queries = []string{titleQ}
	}

	var best *Book
	bestKind := ""
	bestScore := -1
	for _, q := range queries {
		candidates, err := s.queryBooks(ctx,
			`b.id IN (SELECT rowid FROM book_search WHERE book_search MATCH ? LIMIT 200)`,
			"", 0, 0, q)
		if err != nil {
			continue
		}
		for i := range candidates {
			c := &candidates[i]
			got := normalizeForMatch(c.Title)
			if got == "" {
				continue
			}
			kind := MatchExact
			if got != want {
				// Titles shorter than 3 characters do not count as a prefix ("Мы" ≠ "Мысли").
				if len([]rune(want)) < 3 || (!strings.HasPrefix(got, want) && !strings.HasPrefix(want, got)) {
					continue
				}
				kind = MatchFuzzy
			}
			if len(lastNames) > 0 && !authorMatches(c.AuthorNames, lastNames) {
				continue
			}
			// Anthologies with dozens of authors ("Стихотворения", "Рассказы") are
			// not accepted by prefix: it is almost certainly a namesake.
			if kind == MatchFuzzy && strings.Count(c.AuthorNames, ",") >= 3 {
				continue
			}
			diff := len(got) - len(want)
			if diff < 0 {
				diff = -diff
			}
			score := 1000 - diff*10 + int(c.LibRate)
			if kind == MatchExact {
				score += 10000
			}
			if score > bestScore {
				best, bestKind, bestScore = c, kind, score
			}
		}
		if best != nil {
			return best, bestKind
		}
	}
	return nil, ""
}

func authorMatches(authorNames string, lastNames []string) bool {
	low := strings.ToLower(authorNames)
	for _, ln := range lastNames {
		if matchLastName(low, strings.ToLower(ln)) {
			return true
		}
	}
	return false
}
