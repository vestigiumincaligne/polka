package library

import (
	"strings"
	"testing"
)

const textFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description><title-info><book-title>Тест</book-title></title-info></description>
<body>
  <title><p>Тест</p></title>
  <epigraph><p>Эпиграф книги</p><text-author>Автор эпиграфа</text-author></epigraph>
  <section>
    <title><p>Глава первая</p></title>
    <p>Первый абзац с <emphasis>курсивом</emphasis> и сноской<a l:href="#n1" type="note">[1]</a>.</p>
    <empty-line/>
    <poem><stanza><v>Строка стиха раз</v><v>Строка стиха два</v></stanza></poem>
    <image l:href="#pic1.jpg"/>
    <section>
      <title><p>Часть 1.1</p></title>
      <p>Вложенный текст.</p>
    </section>
  </section>
  <section>
    <title><p>Глава вторая</p></title>
    <p>Текст второй главы &amp; амперсанд.</p>
  </section>
</body>
<body name="notes">
  <section id="n1"><title><p>1</p></title><p>Текст сноски.</p></section>
</body>
<binary id="pic1.jpg" content-type="image/jpeg">aGVsbG8=</binary>
</FictionBook>`

func TestParseFB2Text(t *testing.T) {
	text, err := ParseFB2Text(strings.NewReader(textFB2), func(id string) string {
		return "/img/" + id
	})
	if err != nil {
		t.Fatal(err)
	}

	// Preface (the epigraph before the sections) + 2 chapters
	if len(text.Chapters) != 3 {
		t.Fatalf("chapters = %d, want 3 (preface + 2)", len(text.Chapters))
	}
	preface := text.Chapters[0]
	if preface.Title != "" || !strings.Contains(preface.HTML, "Эпиграф книги") {
		t.Errorf("preface = %+v", preface)
	}
	if !strings.Contains(preface.HTML, `class="fb2-epigraph"`) {
		t.Errorf("epigraph class lost: %s", preface.HTML)
	}

	ch1 := text.Chapters[1]
	if ch1.Title != "Глава первая" {
		t.Errorf("ch1 title = %q", ch1.Title)
	}
	for _, want := range []string{
		"<em>курсивом</em>",
		`<sup class="fb2-note-ref"><a data-note="n1">[1]</a></sup>`,
		`<p class="fb2-verse">Строка стиха раз</p>`,
		`<img class="fb2-img" src="/img/pic1.jpg"`,
		"<h3>Часть 1.1</h3>",
		"Вложенный текст.",
	} {
		if !strings.Contains(ch1.HTML, want) {
			t.Errorf("ch1 missing %q in:\n%s", want, ch1.HTML)
		}
	}

	ch2 := text.Chapters[2]
	if ch2.Title != "Глава вторая" || !strings.Contains(ch2.HTML, "&amp; амперсанд") {
		t.Errorf("ch2 = %+v", ch2)
	}

	note, ok := text.Notes["n1"]
	if !ok || !strings.Contains(note, "Текст сноски.") {
		t.Errorf("note n1 = %q", note)
	}
}

func TestExtractBinary(t *testing.T) {
	data, mime, err := ExtractBinary(strings.NewReader(textFB2), "pic1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" || mime != "image/jpeg" {
		t.Errorf("binary = %q %q", data, mime)
	}
	if _, _, err := ExtractBinary(strings.NewReader(textFB2), "nope"); err == nil {
		t.Error("missing binary must fail")
	}
}
