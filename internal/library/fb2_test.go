package library

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 1x1 PNG
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

const testFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description>
  <title-info>
    <genre>sf_fantasy</genre>
    <author><first-name>Иван</first-name><last-name>Иванов</last-name></author>
    <book-title>Тестовая книга</book-title>
    <annotation>
      <p>Первый абзац <emphasis>с курсивом</emphasis>.</p>
      <empty-line/>
      <p>Второй абзац &amp; спецсимволы.</p>
    </annotation>
    <coverpage><image l:href="#cover.png"/></coverpage>
    <lang>ru</lang>
  </title-info>
  <publish-info>
    <publisher>Тестиздат</publisher>
    <city>Москва</city>
    <year>2024</year>
    <isbn>978-5-00000-000-1</isbn>
  </publish-info>
</description>
<body><section><p>Текст книги.</p></section></body>
<binary id="other.png" content-type="image/png">` + tinyPNG + `</binary>
<binary id="cover.png" content-type="image/png">` + tinyPNG + `</binary>
</FictionBook>`

func TestParseFB2Meta(t *testing.T) {
	meta, err := ParseFB2(strings.NewReader(testFB2), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.AnnotationHTML, "<p>Первый абзац <em>с курсивом</em>.</p>") {
		t.Errorf("annotation html = %q", meta.AnnotationHTML)
	}
	if !strings.Contains(meta.AnnotationHTML, "<br/>") {
		t.Errorf("empty-line not converted: %q", meta.AnnotationHTML)
	}
	if !strings.Contains(meta.AnnotationHTML, "&amp; спецсимволы") {
		t.Errorf("text not escaped: %q", meta.AnnotationHTML)
	}
	if meta.Publisher != "Тестиздат" || meta.City != "Москва" || meta.Year != "2024" || meta.ISBN != "978-5-00000-000-1" {
		t.Errorf("publish info: %+v", meta)
	}
	if len(meta.Cover) != 0 {
		t.Error("cover must not be read when withCover=false")
	}
}

func TestParseFB2Cover(t *testing.T) {
	meta, err := ParseFB2(strings.NewReader(testFB2), true)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := base64.StdEncoding.DecodeString(tinyPNG)
	if string(meta.Cover) != string(want) {
		t.Errorf("cover bytes mismatch: got %d bytes", len(meta.Cover))
	}
	if meta.CoverMime != "image/png" {
		t.Errorf("cover mime = %q", meta.CoverMime)
	}
}

func TestParseFB2CP1251(t *testing.T) {
	// windows-1251: the Russian word "Test" encoded in cp1251
	cp1251Body := `<?xml version="1.0" encoding="windows-1251"?>
<FictionBook><description><title-info>
<annotation><p>` + string([]byte{0xD2, 0xE5, 0xF1, 0xF2}) + `</p></annotation>
</title-info></description></FictionBook>`
	meta, err := ParseFB2(strings.NewReader(cp1251Body), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta.AnnotationHTML, "Тест") {
		t.Errorf("cp1251 not decoded: %q", meta.AnnotationHTML)
	}
}

func TestStripBinaries(t *testing.T) {
	out := string(StripBinaries([]byte(testFB2)))
	if strings.Contains(out, "<binary") {
		t.Error("binaries not stripped")
	}
	if !strings.Contains(out, "Текст книги") {
		t.Error("body damaged")
	}
}
