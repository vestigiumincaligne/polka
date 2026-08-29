// Package sources provides external sources of book collections: sites
// from which Polka periodically scrapes "author + title" lists and turns
// them into collections. Sources are disabled by default and enabled by
// the administrator.
package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vestigiumincaligne/polka/internal/collections"
)

// Source is an external source of collections.
type Source interface {
	// ID is the source key (used as Collection.Origin and in settings).
	ID() string
	// Fetch returns the source's collections, skipping already known ones
	// (known holds slugs loaded earlier: their pages are not re-downloaded).
	Fetch(ctx context.Context, known map[string]bool) ([]*collections.File, error)
}

// All lists all known sources (the order is used by the UI).
func All() []Source {
	return []Source{NewForbes("")}
}

// ByID returns the source with the given key or nil.
func ByID(id string) Source {
	for _, s := range All() {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/125.0 polka-collections"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// get downloads a page (up to 4 MB), identifying as a browser.
func get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru,en;q=0.7")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", rawURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
