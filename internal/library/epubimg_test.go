package library

import (
	"os"
	"regexp"
	"testing"
)

func TestEPUBImageDimensions(t *testing.T) {
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
	imgs, withDims := 0, 0
	re := regexp.MustCompile(`<img [^>]*width="\d+" height="\d+"`)
	reAny := regexp.MustCompile(`<img `)
	for _, ch := range text.Chapters {
		imgs += len(reAny.FindAllString(ch.HTML, -1))
		withDims += len(re.FindAllString(ch.HTML, -1))
	}
	t.Logf("images: %d, with width/height: %d", imgs, withDims)
	if imgs > 0 && withDims == 0 {
		t.Error("no image got dimensions")
	}
}
