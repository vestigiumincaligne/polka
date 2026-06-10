package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const fantlabSearch = `{"matches":[
  {"work_id":569,"rusname":"Пикник на обочине","all_autor_rusname":"Аркадий и Борис Стругацкие"}
]}`

const fantlabSimilars = `[
  {"rusname":"Солярис","creators":{"authors":[{"name":"Станислав Лем"}]}},
  {"rusname":"Улитка на склоне","creators":{"authors":[{"name":"Аркадий и Борис Стругацкие"}]}}
]`

const tastediveJSON = `{"similar":{"results":[
  {"name":"Roadside Picnic","type":"book"},
  {"name":"Solaris","type":"book"},
  {"name":"Stalker","type":"movie"}
]}}`

func TestSimilarSources(t *testing.T) {
	fl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search-works"):
			w.Write([]byte(fantlabSearch))
		case strings.HasSuffix(r.URL.Path, "/similars"):
			w.Write([]byte(fantlabSimilars))
		default:
			http.NotFound(w, r)
		}
	}))
	td := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("k") != "goodkey" {
			w.Write([]byte(`{"error":"Invalid api key format"}`))
			return
		}
		w.Write([]byte(tastediveJSON))
	}))
	t.Cleanup(fl.Close)
	t.Cleanup(td.Close)

	p := New(filepath.Join(t.TempDir(), "cache.json"))
	p.FantLabBase = fl.URL
	p.TasteDiveBase = td.URL

	cfg := SimilarConfig{FantLab: true, TasteDive: true, TasteDiveKey: "goodkey"}
	out := p.Similar(context.Background(), "sim:1", "Пикник на обочине", "Стругацкие", cfg)

	var fantlab, tastedive int
	for _, s := range out {
		switch s.Source {
		case SourceFantLab:
			fantlab++
		case SourceTasteDive:
			tastedive++
			if s.Title == "Stalker" {
				t.Error("movie must be filtered out")
			}
		}
	}
	if fantlab != 2 || tastedive != 2 {
		t.Errorf("fantlab=%d tastedive=%d, want 2/2: %+v", fantlab, tastedive, out)
	}
	if out[0].Title != "Солярис" || out[0].Author != "Станислав Лем" {
		t.Errorf("first = %+v", out[0])
	}

	// Cache: a repeated call with sources disabled returns the saved data
	out2 := p.Similar(context.Background(), "sim:1", "Пикник на обочине", "Стругацкие", SimilarConfig{})
	if len(out2) != len(out) {
		t.Errorf("cache miss: %d != %d", len(out2), len(out))
	}
}

func TestSimilarDisabled(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "cache.json"))
	p.FantLabBase = "http://127.0.0.1:1"
	p.TasteDiveBase = "http://127.0.0.1:1"
	out := p.Similar(context.Background(), "sim:2", "Книга", "", SimilarConfig{})
	if len(out) != 0 {
		t.Errorf("disabled sources returned %v", out)
	}
}
