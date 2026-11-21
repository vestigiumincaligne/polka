package server

import (
	"mime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBookFileName(t *testing.T) {
	cases := []struct {
		name string
		p    fileNameParts
		ext  string
		want string
	}{
		{"full", fileNameParts{ID: 478902, Authors: []string{"Сергей Лукьяненко"}, Series: "Дозоры", SeqNumber: 1, Title: "Ночной Дозор"}, "epub",
			"478902 - Сергей Лукьяненко - Дозоры - 01 - Ночной Дозор.epub"},
		{"no series", fileNameParts{ID: 7, Authors: []string{"Сергей Лукьяненко"}, Title: "Ночной Дозор"}, "fb2",
			"7 - Сергей Лукьяненко - Ночной Дозор.fb2"},
		{"series without number", fileNameParts{ID: 7, Authors: []string{"A B"}, Series: "S", Title: "T"}, "fb2",
			"7 - A B - S - T.fb2"},
		{"number without series is dropped", fileNameParts{ID: 7, SeqNumber: 3, Title: "T"}, "fb2", "7 - T.fb2"},
		{"garbage number", fileNameParts{ID: 7, Series: "S", SeqNumber: 19870607, Title: "T"}, "fb2", "7 - S - T.fb2"},
		{"large number", fileNameParts{ID: 7, Series: "S", SeqNumber: 123, Title: "T"}, "fb2", "7 - S - 123 - T.fb2"},
		{"two authors", fileNameParts{ID: 1, Authors: []string{"Илья Ильф", "Евгений Петров"}, Title: "12 стульев"}, "fb2",
			"1 - Илья Ильф, Евгений Петров - 12 стульев.fb2"},
		{"many authors", fileNameParts{ID: 1, Authors: []string{"A", "B", "C"}, Title: "T"}, "fb2", "1 - A и др. - T.fb2"},
		{"forbidden chars", fileNameParts{ID: 1, Authors: []string{"Автор"}, Title: `Что? Где: "Когда" <a/b> | c\d*`}, "fb2",
			"1 - Автор - Что Где Когда a b c d.fb2"},
		{"no redundant separators", fileNameParts{ID: 1, Authors: []string{"  ", "-"}, Series: " ... ", Title: " - Название. "}, "fb2",
			"1 - Название.fb2"},
		{"control chars", fileNameParts{ID: 1, Title: "a\x00b\tc​d"}, "fb2", "1 - ab cd.fb2"},
		{"fallback to file", fileNameParts{ID: 1, Fallback: "749871"}, "fb2", "1 - 749871.fb2"},
		{"empty", fileNameParts{}, "fb2", "book.fb2"},
		{"reserved", fileNameParts{Title: "CON"}, "fb2", "CON_.fb2"},
	}
	for _, c := range cases {
		if got := bookFileName(c.p, c.ext); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBookFileNameLength(t *testing.T) {
	p := fileNameParts{ID: 123456, Authors: []string{"Сергей Лукьяненко"}, Series: "Дозоры", SeqNumber: 2,
		Title: strings.Repeat("Очень длинное название ", 30)}
	name := bookBaseName(p)
	if len(name) > maxFileNameBytes || !utf8.ValidString(name) {
		t.Fatalf("bad length/encoding: %d %q", len(name), name)
	}
	if !strings.HasPrefix(name, "123456 - Сергей Лукьяненко - Дозоры - 02 - Очень") {
		t.Errorf("prefix lost: %q", name)
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, "-") {
		t.Errorf("junk at the end: %q", name)
	}
}

func TestTruncateUTF8(t *testing.T) {
	s := "абв" // 6 bytes
	for n := 0; n <= 7; n++ {
		if got := truncateUTF8(s, n); !utf8.ValidString(got) || len(got) > n {
			t.Errorf("truncate %d: %q", n, got)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	v := contentDisposition("attachment", "1 - Лев Толстой - Война и мир.fb2", "1.fb2")
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		t.Fatalf("%q: %v", v, err)
	}
	if params["filename"] != "1 - Лев Толстой - Война и мир.fb2" {
		t.Errorf("decoded filename: %q (header %q)", params["filename"], v)
	}
	if !strings.Contains(v, `filename="1.fb2"`) {
		t.Errorf("no ASCII fallback: %q", v)
	}

	v = contentDisposition("inline", "1 - Author - Title.pdf", "")
	if v != `inline; filename="1 - Author - Title.pdf"` {
		t.Errorf("ascii name: %q", v)
	}
}
