package library

import (
	"os"
	"strings"
	"testing"
)

func TestEPUBTextOnRealFile(t *testing.T) {
	path := os.Getenv("EPUB")
	if path == "" {
		t.Skip("set EPUB env to a real file")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	text, err := EPUBText(f, st.Size(), func(p string) string { return "/img/" + p })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("chapters: %d, first title: %q", len(text.Chapters), text.Chapters[0].Title)
	if len(text.Chapters) < 3 {
		t.Errorf("too few chapters: %d", len(text.Chapters))
	}
	all := ""
	for _, ch := range text.Chapters {
		all += ch.HTML
	}
	for _, bad := range []string{"<script", "<style", "onclick="} {
		if strings.Contains(strings.ToLower(all), bad) {
			t.Errorf("unsanitized %q in output", bad)
		}
	}
}
