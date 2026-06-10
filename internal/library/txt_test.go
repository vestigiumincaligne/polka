package library

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestParseTXTUTF8(t *testing.T) {
	src := "Первый абзац.\n\nВторой абзац <с> спецсимволами.\n\nТретий."
	text, err := ParseTXT(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Chapters) != 1 {
		t.Fatalf("chapters = %d", len(text.Chapters))
	}
	html := text.Chapters[0].HTML
	for _, want := range []string{"<p>Первый абзац.</p>", "&lt;с&gt; спецсимволами"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
}

func TestParseTXTCP1251(t *testing.T) {
	enc, _ := charmap.Windows1251.NewEncoder().String("Тест кодировки.\n\nВторой абзац.")
	text, err := ParseTXT(strings.NewReader(enc))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.Chapters[0].HTML, "Тест кодировки.") {
		t.Errorf("cp1251 not decoded: %s", text.Chapters[0].HTML)
	}
}

func TestParseTXTChunks(t *testing.T) {
	// Long text gets split into "parts"
	para := strings.Repeat("слово ", 1000) // ~6KB
	src := strings.Repeat(para+"\n\n", 20) // ~120KB
	text, err := ParseTXT(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Chapters) < 2 {
		t.Errorf("long text must split: chapters = %d", len(text.Chapters))
	}

}
