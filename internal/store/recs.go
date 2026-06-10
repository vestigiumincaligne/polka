package store

import (
	"context"
	"fmt"
	"strings"
)

// Local recommender: similar books and personal shelves based on
// the collection's genres, authors, series, and keywords.

// SimilarBooks picks books similar to the given one: shared keywords (a strong
// signal), genre overlap, the same author; a boost for rating.
// Books from the same series are excluded — they are already on the card.
func (s *Store) SimilarBooks(ctx context.Context, bookID int64, limit int) ([]Book, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH seed AS (SELECT series_id FROM books WHERE id = ?1),
		seed_genres AS (SELECT genre_id FROM book_genres WHERE book_id = ?1),
		seed_kw AS (SELECT keyword_id FROM book_keywords WHERE book_id = ?1),
		seed_authors AS (SELECT author_id FROM book_authors WHERE book_id = ?1),
		-- Candidates: shared keywords, same author, or >=2 shared genres
		-- among high-rated books (broad genres are gated by rating).
		cand AS (
			SELECT bk.book_id AS id FROM book_keywords bk
				JOIN seed_kw USING (keyword_id)
			UNION
			SELECT ba.book_id FROM book_authors ba
				JOIN seed_authors USING (author_id)
			UNION
			SELECT bg.book_id FROM book_genres bg
				JOIN seed_genres USING (genre_id)
				JOIN books b ON b.id = bg.book_id AND b.lib_rate >= 4
			GROUP BY bg.book_id HAVING count(*) >= 2
		)
		SELECT b.id, b.title,
			coalesce((SELECT group_concat(trim(a.last_name || ' ' || a.first_name), ', ')
			          FROM book_authors ba JOIN authors a ON a.id = ba.author_id
			          WHERE ba.book_id = b.id), ''),
			coalesce(s2.title, ''), coalesce(b.series_num, 0), coalesce(b.year, 0),
			b.lib_rate, b.ext, b.size,
			(SELECT count(*) FROM book_keywords bk JOIN seed_kw USING (keyword_id) WHERE bk.book_id = b.id) * 3.0
			+ (SELECT count(*) FROM book_genres bg JOIN seed_genres USING (genre_id) WHERE bg.book_id = b.id) * 2.0
			+ (SELECT count(*) FROM book_authors ba JOIN seed_authors USING (author_id) WHERE ba.book_id = b.id) * 1.2
			+ b.lib_rate * 0.6 AS score
		FROM books b
		JOIN cand ON cand.id = b.id
		LEFT JOIN series s2 ON s2.id = b.series_id
		WHERE b.id != ?1 AND b.deleted = 0
			AND (b.series_id IS NULL OR b.series_id IS NOT (SELECT series_id FROM seed))
			-- skip translations and foreign-language editions
			AND ((SELECT lang FROM books WHERE id = ?1) = '' OR b.lang = (SELECT lang FROM books WHERE id = ?1))
		ORDER BY score DESC, b.lib_rate DESC
		LIMIT ?2`, bookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		var score float64
		if err := rows.Scan(&b.ID, &b.Title, &b.AuthorNames, &b.SeriesTitle,
			&b.SeqNumber, &b.Year, &b.LibRate, &b.Ext, &b.Size, &score); err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	return books, rows.Err()
}

// SeriesContinuations: for finished books that belong to series, the next book in the series.
func (s *Store) SeriesContinuations(ctx context.Context, seedIDs, excludeIDs []int64, limit int) ([]Book, error) {
	if len(seedIDs) == 0 {
		return nil, nil
	}
	q := fmt.Sprintf(`
		WITH seeds AS (
			SELECT series_id, max(series_num) AS last_num
			FROM books WHERE id IN (%s) AND series_id IS NOT NULL AND series_num IS NOT NULL
			GROUP BY series_id
		)
		SELECT b.id FROM seeds
		JOIN books b ON b.series_id = seeds.series_id AND b.deleted = 0
			AND b.series_num = (
				SELECT min(b2.series_num) FROM books b2
				WHERE b2.series_id = seeds.series_id AND b2.deleted = 0
					AND b2.series_num > seeds.last_num
					%s
			)
		LIMIT %d`,
		placeholders(len(seedIDs)), notInClause("b2.id", len(excludeIDs), len(seedIDs)+1), limit)

	args := idsToArgs(seedIDs)
	args = append(args, idsToArgs(excludeIDs)...)
	ids, err := s.queryIDs(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return s.BooksByIDs(ctx, ids)
}

// RecommendForUser: unread books by favorite authors and the top of favorite
// genres (based on seeds — liked/currently-read books).
func (s *Store) RecommendForUser(ctx context.Context, seedIDs, excludeIDs []int64, limit int) ([]Book, error) {
	if len(seedIDs) == 0 {
		return nil, nil
	}
	half := limit/2 + 1

	// Books by favorite authors (best by rating)
	qAuthors := fmt.Sprintf(`
		WITH seed_authors AS (
			SELECT DISTINCT author_id FROM book_authors WHERE book_id IN (%s)
		)
		SELECT DISTINCT b.id FROM book_authors ba
		JOIN seed_authors USING (author_id)
		JOIN books b ON b.id = ba.book_id AND b.deleted = 0
		WHERE b.id NOT IN (%s) %s
		ORDER BY b.lib_rate DESC, b.id DESC LIMIT %d`,
		placeholders(len(seedIDs)), placeholders2(len(seedIDs), len(seedIDs)),
		notInClause("b.id", len(excludeIDs), 2*len(seedIDs)+1), half)
	args := idsToArgs(seedIDs)
	args = append(args, idsToArgs(seedIDs)...)
	args = append(args, idsToArgs(excludeIDs)...)
	byAuthor, err := s.queryIDs(ctx, qAuthors, args...)
	if err != nil {
		return nil, err
	}

	// Top-rated in favorite genres (genres are counted by frequency among seeds)
	qGenres := fmt.Sprintf(`
		WITH fav_genres AS (
			SELECT genre_id FROM book_genres WHERE book_id IN (%s)
			GROUP BY genre_id ORDER BY count(*) DESC LIMIT 4
		)
		SELECT b.id FROM books b
		WHERE b.lib_rate >= 4 AND b.deleted = 0
			AND b.id IN (SELECT book_id FROM book_genres JOIN fav_genres USING (genre_id))
			AND b.id NOT IN (%s) %s
		ORDER BY b.lib_rate DESC, b.id DESC LIMIT %d`,
		placeholders(len(seedIDs)), placeholders2(len(seedIDs), len(seedIDs)),
		notInClause("b.id", len(excludeIDs), 2*len(seedIDs)+1), half)
	byGenre, err := s.queryIDs(ctx, qGenres, args...)
	if err != nil {
		return nil, err
	}

	// Interleave the sources, drop duplicates
	seen := map[int64]bool{}
	var ids []int64
	for i := 0; i < len(byAuthor) || i < len(byGenre); i++ {
		for _, src := range [][]int64{byAuthor, byGenre} {
			if i < len(src) && !seen[src[i]] {
				seen[src[i]] = true
				ids = append(ids, src[i])
			}
		}
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return s.BooksByIDs(ctx, ids)
}

// NormalizeISBN keeps only digits and X (the ISBN-10 check character).
func NormalizeISBN(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= '0' && r <= '9') || r == 'X' {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) != 10 && len(n) != 13 {
		return ""
	}
	return n
}

// LooksLikeISBN reports whether the query looks like an ISBN (10/13 characters after normalization).
func LooksLikeISBN(query string) bool {
	for _, r := range query {
		if !(r >= '0' && r <= '9') && r != 'x' && r != 'X' && r != '-' && r != ' ' {
			return false
		}
	}
	return NormalizeISBN(query) != ""
}

// SearchByISBN finds books by their stored ISBN.
func (s *Store) SearchByISBN(ctx context.Context, isbn string) ([]Book, error) {
	n := NormalizeISBN(isbn)
	if n == "" {
		return nil, nil
	}
	return s.queryBooks(ctx, `b.isbn = ?`, `b.title`, 50, 0, n)
}

// SetBookISBN records a book's ISBN (lazy indexing on view).
func (s *Store) SetBookISBN(ctx context.Context, bookID int64, isbn string) {
	if n := NormalizeISBN(isbn); n != "" {
		s.db.ExecContext(ctx, `UPDATE books SET isbn = ? WHERE id = ? AND isbn = ''`, n, bookID)
	}
}

// MatchBook finds a collection book by title and author (for mapping
// external recommendations); returns ErrNotFound if there is no confident match.
func (s *Store) MatchBook(ctx context.Context, title, author string) (*Book, error) {
	var lastNames []string
	if author != "" {
		fields := strings.Fields(author)
		// external sources use "First Last" order — take both ends
		lastNames = append(lastNames, fields[0])
		if len(fields) > 1 {
			lastNames = append(lastNames, fields[len(fields)-1])
		}
	}
	matches, err := s.FindSimilar(ctx, title, nil)
	if err != nil || len(matches) == 0 {
		return nil, ErrNotFound
	}
	if len(lastNames) == 0 {
		return &matches[0], nil
	}
	for _, m := range matches {
		low := strings.ToLower(m.AuthorNames)
		for _, ln := range lastNames {
			if matchLastName(low, strings.ToLower(ln)) {
				return &m, nil
			}
		}
	}
	return nil, ErrNotFound
}

// matchLastName compares a last name allowing for declensions and forms
// (e.g. "Strugatskie" ~ "Strugatsky"): exact substring or a shared prefix.
func matchLastName(authorNames, lastName string) bool {
	if strings.Contains(authorNames, lastName) {
		return true
	}
	runes := []rune(lastName)
	if len(runes) < 6 {
		return false
	}
	return strings.Contains(authorNames, string(runes[:len(runes)-2]))
}

// --- helpers ---

func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.Repeat("?,", n-1) + "?"
}

// placeholders2 is the same, but for the second occurrence of the same set.
func placeholders2(n, _ int) string { return placeholders(n) }

func notInClause(col string, n, _ int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("AND %s NOT IN (%s)", col, placeholders(n))
}

func idsToArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func (s *Store) queryIDs(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
