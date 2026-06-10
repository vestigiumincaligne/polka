package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Similar books from external sources: FantLab (Russian-language,
// open API) and TasteDive (international, requires a free key).

const (
	SourceFantLab   = "fantlab"
	SourceTasteDive = "tastedive"
)

type SimilarBook struct {
	Title  string `json:"title"`
	Author string `json:"author,omitempty"`
	Source string `json:"source"`
}

type SimilarConfig struct {
	FantLab      bool
	TasteDive    bool
	TasteDiveKey string
}

type similarEntry struct {
	Results   []SimilarBook `json:"results"`
	FetchedAt time.Time     `json:"fetchedAt"`
}

// Similar returns similar books from the enabled sources (cached).
func (p *Provider) Similar(ctx context.Context, key, title, author string, cfg SimilarConfig) []SimilarBook {
	p.mu.Lock()
	if p.similar == nil {
		p.loadSimilarCache()
	}
	if e, ok := p.similar[key]; ok && time.Since(e.FetchedAt) < successTTL {
		p.mu.Unlock()
		return e.Results
	}
	p.mu.Unlock()

	var out []SimilarBook
	transient := false

	if cfg.FantLab {
		if res, err := p.fantlabSimilar(ctx, title, author); err != nil {
			transient = true
			p.log("fantlab similar", err)
		} else {
			out = append(out, res...)
		}
	}
	if cfg.TasteDive && cfg.TasteDiveKey != "" {
		if res, err := p.tastediveSimilar(ctx, title, author, cfg.TasteDiveKey); err != nil {
			transient = true
			p.log("tastedive similar", err)
		} else {
			out = append(out, res...)
		}
	}

	if transient && len(out) == 0 {
		return out // network failures are not cached
	}
	p.mu.Lock()
	p.similar[key] = similarEntry{Results: out, FetchedAt: time.Now()}
	p.persistSimilarCache()
	p.mu.Unlock()
	return out
}

func (p *Provider) similarCachePath() string { return p.cachePath + ".similar" }

func (p *Provider) loadSimilarCache() {
	p.similar = map[string]similarEntry{}
	data, err := os.ReadFile(p.similarCachePath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &p.similar)
}

func (p *Provider) persistSimilarCache() {
	data, err := json.Marshal(p.similar)
	if err != nil {
		return
	}
	tmp := p.similarCachePath() + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p.similarCachePath())
	}
}

func (p *Provider) log(msg string, err error) {
	// The provider keeps no logger; similar-books failures are not critical.
	_ = msg
	_ = err
}

// --- FantLab ---

const similarLimit = 10

func (p *Provider) fantlabSimilar(ctx context.Context, title, author string) ([]SimilarBook, error) {
	// 1. Find the work
	body, err := p.get(ctx, p.FantLabBase+"/search-works?q="+url.QueryEscape(title), false)
	if err != nil {
		return nil, err
	}
	var search struct {
		Matches []struct {
			WorkID   json.Number `json:"work_id"`
			RusName  string      `json:"rusname"`
			Name     string      `json:"name"`
			AutorRus string      `json:"autor_rusname"`
			AllRus   string      `json:"all_autor_rusname"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, err
	}
	if len(search.Matches) == 0 {
		return nil, nil
	}

	// Prefer an author match (the last name occurs in the FantLab author name).
	pick := search.Matches[0]
	if author != "" {
		last := strings.Fields(author)[0]
		for _, m := range search.Matches {
			if strings.Contains(strings.ToLower(m.AllRus+m.AutorRus), strings.ToLower(last)) {
				pick = m
				break
			}
		}
	}
	if pick.WorkID.String() == "" || pick.WorkID.String() == "0" {
		return nil, nil
	}

	// 2. Similar works
	body, err = p.get(ctx, p.FantLabBase+"/work/"+pick.WorkID.String()+"/similars", false)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var out []SimilarBook
	for _, item := range raw {
		name, _ := item["rusname"].(string)
		if name == "" {
			name, _ = item["name"].(string)
		}
		if name == "" {
			continue
		}
		var authorName string
		if creators, ok := item["creators"].(map[string]any); ok {
			if authors, ok := creators["authors"].([]any); ok && len(authors) > 0 {
				if a, ok := authors[0].(map[string]any); ok {
					authorName, _ = a["name"].(string)
				}
			}
		}
		out = append(out, SimilarBook{Title: name, Author: authorName, Source: SourceFantLab})
		if len(out) >= similarLimit {
			break
		}
	}
	return out, nil
}

// --- Resolving ISBN -> title+author (for ISBN search) ---

type ISBNInfo struct {
	Title  string
	Author string
}

// ResolveISBN identifies a book by ISBN: Open Library, then Google Books.
func (p *Provider) ResolveISBN(ctx context.Context, isbn string) (*ISBNInfo, error) {
	if info, err := p.isbnOpenLibrary(ctx, isbn); err == nil && info != nil {
		return info, nil
	}
	return p.isbnGoogle(ctx, isbn)
}

func (p *Provider) isbnOpenLibrary(ctx context.Context, isbn string) (*ISBNInfo, error) {
	body, err := p.get(ctx, p.OpenLibraryBase+"/search.json?fields=title,author_name&limit=1&isbn="+url.QueryEscape(isbn), false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Docs []struct {
			Title      string   `json:"title"`
			AuthorName []string `json:"author_name"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Docs) == 0 || data.Docs[0].Title == "" {
		return nil, fmt.Errorf("not found")
	}
	info := &ISBNInfo{Title: data.Docs[0].Title}
	if len(data.Docs[0].AuthorName) > 0 {
		info.Author = data.Docs[0].AuthorName[0]
	}
	return info, nil
}

func (p *Provider) isbnGoogle(ctx context.Context, isbn string) (*ISBNInfo, error) {
	body, err := p.get(ctx, p.GoogleBase+"/books/v1/volumes?maxResults=1&q="+url.QueryEscape("isbn:"+isbn), false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []struct {
			VolumeInfo struct {
				Title   string   `json:"title"`
				Authors []string `json:"authors"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &data); err != nil || len(data.Items) == 0 || data.Items[0].VolumeInfo.Title == "" {
		return nil, fmt.Errorf("not found")
	}
	info := &ISBNInfo{Title: data.Items[0].VolumeInfo.Title}
	if len(data.Items[0].VolumeInfo.Authors) > 0 {
		info.Author = data.Items[0].VolumeInfo.Authors[0]
	}
	return info, nil
}

// --- TasteDive ---

func (p *Provider) tastediveSimilar(ctx context.Context, title, author, apiKey string) ([]SimilarBook, error) {
	q := title
	if author != "" {
		q = title + " " + author
	}
	u := fmt.Sprintf("%s/api/similar?type=book&limit=%d&k=%s&q=%s",
		p.TasteDiveBase, similarLimit, url.QueryEscape(apiKey), url.QueryEscape("book:"+q))
	body, err := p.get(ctx, u, false)
	if err != nil {
		return nil, err
	}
	var data struct {
		Similar struct {
			Results []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"results"`
		} `json:"similar"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	var out []SimilarBook
	for _, r := range data.Similar.Results {
		if r.Type != "book" || r.Name == "" {
			continue
		}
		out = append(out, SimilarBook{Title: r.Name, Source: SourceTasteDive})
	}
	return out, nil
}
