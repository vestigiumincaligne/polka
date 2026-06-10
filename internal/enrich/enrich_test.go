package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

const livelibHTML = `<html><body>
<a href="/book/1017090091-master-i-margarita"><img/></a>
<div class="rating-book marg-right"><span class="rating-value stars-color-orange" itemprop="ratingValue">4,5</span></div>
</body></html>`

const googleJSON = `{"items":[
  {"volumeInfo":{"title":"Без рейтинга"}},
  {"volumeInfo":{"title":"Мастер и Маргарита","averageRating":4.2,"ratingsCount":128,
    "infoLink":"http://books.google.com/books?id=x",
    "imageLinks":{"thumbnail":"http://books.google.com/cover.jpg"}}}
]}`

const openLibJSON = `{"docs":[
  {"key":"/works/OL1W","title":"Master","ratings_average":3.9,"ratings_count":55,"cover_i":777}
]}`

func newTestProvider(t *testing.T, livelibOK, googleOK, openlibOK bool) *Provider {
	t.Helper()
	handler := func(ok bool, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			if !ok {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(body))
		}
	}
	ll := httptest.NewServer(handler(livelibOK, livelibHTML))
	gb := httptest.NewServer(handler(googleOK, googleJSON))
	ol := httptest.NewServer(handler(openlibOK, openLibJSON))
	t.Cleanup(ll.Close)
	t.Cleanup(gb.Close)
	t.Cleanup(ol.Close)

	p := New(filepath.Join(t.TempDir(), "cache.json"))
	p.LiveLibBase = ll.URL
	p.GoogleBase = gb.URL
	p.OpenLibraryBase = ol.URL
	return p
}

func allEnabled() map[string]bool {
	return map[string]bool{SourceLiveLib: true, SourceGoogleBooks: true, SourceOpenLibrary: true}
}

func TestEnrichAllSources(t *testing.T) {
	p := newTestProvider(t, true, true, true)
	res := p.Get(context.Background(), "1", "Мастер и Маргарита", "Булгаков", allEnabled())

	if len(res.Sources) != 3 || res.Negative {
		t.Fatalf("sources = %d, negative = %v", len(res.Sources), res.Negative)
	}
	if res.Primary.Source != SourceLiveLib || res.Primary.Rating != 4.5 {
		t.Errorf("primary = %+v", res.Primary)
	}
	if res.ExtraSourceCount != 2 {
		t.Errorf("extraSourceCount = %d", res.ExtraSourceCount)
	}
	// Cover from the first source that has one (google)
	if res.CoverURL != "https://books.google.com/cover.jpg" {
		t.Errorf("coverUrl = %q", res.CoverURL)
	}
	gb := res.Sources[1]
	if gb.Source != SourceGoogleBooks || gb.RatingsCount != 128 {
		t.Errorf("google = %+v", gb)
	}
	ol := res.Sources[2]
	if ol.Source != SourceOpenLibrary || ol.CoverURL != "https://covers.openlibrary.org/b/id/777-M.jpg" {
		t.Errorf("openlib = %+v", ol)
	}
}

func TestEnrichDisabledSources(t *testing.T) {
	p := newTestProvider(t, true, true, true)
	enabled := allEnabled()
	enabled[SourceLiveLib] = false
	res := p.Get(context.Background(), "2", "Мастер и Маргарита", "", enabled)
	if res.Primary == nil || res.Primary.Source != SourceGoogleBooks {
		t.Errorf("primary with livelib off = %+v", res.Primary)
	}
	for _, src := range res.Sources {
		if src.Source == SourceLiveLib {
			t.Error("disabled source must not be queried")
		}
	}
}

func TestEnrichTransientNotCached(t *testing.T) {
	p := newTestProvider(t, false, false, false)
	res := p.Get(context.Background(), "3", "Книга", "", allEnabled())
	if !res.Negative {
		t.Error("all sources down -> negative response")
	}
	if _, cached := p.cache["3"]; cached {
		t.Error("transient failure must not be cached")
	}
}

func TestEnrichCachePersistence(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	p := newTestProvider(t, true, true, true)
	p.cachePath = cachePath
	p.Get(context.Background(), "5", "Мастер и Маргарита", "", allEnabled())

	// A new provider reads the cache from disk and does not hit the network.
	p2 := New(cachePath)
	p2.LiveLibBase = "http://127.0.0.1:1" // intentionally unreachable
	res := p2.Get(context.Background(), "5", "Мастер и Маргарита", "", allEnabled())
	if res.Primary == nil || res.Primary.Rating != 4.5 {
		t.Errorf("cached result = %+v", res.Primary)
	}
}
