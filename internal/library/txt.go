package library

import (
	"fmt"
	"html"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	txtMaxSize      = 32 << 20 // protective size cap
	txtChapterChars = 40_000   // approximate "chapter" size in characters
)

// ParseTXT turns a plain-text file into chapters for the reader.
// Non-UTF-8 files are assumed to be windows-1251 (the de facto standard for Russian txt).
func ParseTXT(r io.Reader) (*FB2Text, error) {
	raw, err := io.ReadAll(io.LimitReader(r, txtMaxSize))
	if err != nil {
		return nil, fmt.Errorf("txt read: %w", err)
	}

	text := string(raw)
	if !utf8.Valid(raw) {
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(raw)
		if err != nil {
			return nil, fmt.Errorf("txt decode: %w", err)
		}
		text = string(decoded)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimPrefix(text, "\uFEFF")

	// Paragraphs: split on blank lines; if there are none (one line = one
	// paragraph), split on single newlines.
	paragraphs := splitParagraphs(text)

	res := &FB2Text{Notes: map[string]string{}}
	var sb strings.Builder
	var chars int
	flush := func() {
		if sb.Len() == 0 {
			return
		}
		res.Chapters = append(res.Chapters, FB2Chapter{HTML: sb.String()})
		sb.Reset()
		chars = 0
	}
	for _, p := range paragraphs {
		sb.WriteString("<p>" + html.EscapeString(p) + "</p>")
		chars += len(p)
		if chars >= txtChapterChars {
			flush()
		}
	}
	flush()

	if len(res.Chapters) == 0 {
		return nil, fmt.Errorf("txt: empty file")
	}
	return res, nil
}

func splitParagraphs(text string) []string {
	var blocks []string
	if strings.Contains(text, "\n\n") {
		blocks = strings.Split(text, "\n\n")
	} else {
		blocks = strings.Split(text, "\n")
	}
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		b = strings.TrimSpace(strings.ReplaceAll(b, "\n", " "))
		if b != "" {
			out = append(out, b)
		}
	}
	return out
}
