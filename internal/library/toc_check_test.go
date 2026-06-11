package library

import (
	"os"
	"strings"
	"testing"
)

func TestEPUBTocLinks(t *testing.T) {
	path := os.Getenv("EPUB")
	if path == "" {
		t.Skip("set EPUB")
	}
	f, _ := os.Open(path)
	defer f.Close()
	st, _ := f.Stat()
	text, err := EPUBText(f, st.Size(), func(p string) string { return "/img/" + p })
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	for _, ch := range text.Chapters {
		links += strings.Count(ch.HTML, "data-goto=")
		if strings.Contains(ch.HTML, "data-doc=") {
			t.Error("unresolved data-doc placeholder")
		}
	}
	t.Logf("internal links: %d", links)
	if links == 0 {
		t.Error("no toc links survived")
	}
}
