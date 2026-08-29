package library

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures: zip archives and folders are built in a temp root; 7z archives
// (Go has no 7z writer) come from testdata/:
//
//	books.7z  — 1.fb2 and an extensionless "2" (Flibusta id-only naming)
//	covers.7z — "1": a PNG with no extension (type must be sniffed)
//	epub.7z   — a 7z-packed epub under a "mybook/" prefix (Flibusta epub dumps)

var pngBytes, _ = base64.StdEncoding.DecodeString(tinyPNG)

func writeZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, _ := zw.Create(name)
		w.Write(data)
	}
	zw.Close()
	f.Close()
}

func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, data, 0o644)
}

// epubZip builds a minimal epub; epub2 switches the cover pointer from the
// EPUB3 manifest property to the EPUB2 <meta name="cover"> form.
func epubZip(t *testing.T, epub2 bool, withCover bool) []byte {
	t.Helper()
	coverItem := `<item id="cov" href="img/cover.png" media-type="image/png" properties="cover-image"/>`
	metaCover := ""
	if epub2 {
		coverItem = `<item id="cov" href="img/cover.png" media-type="image/png"/>`
		metaCover = `<meta name="cover" content="cov"/>`
	}
	if !withCover {
		coverItem, metaCover = "", ""
	}
	opf := `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title> Test Epub </dc:title><dc:creator>Ann Author</dc:creator><dc:creator> </dc:creator>
<dc:language>EN</dc:language><dc:subject>Science fiction</dc:subject>` + metaCover + `</metadata>
<manifest><item id="ch1" href="ch1.xhtml" media-type="application/xhtml+xml"/><item id="ch2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
<item id="empty" href="empty.xhtml" media-type="application/xhtml+xml"/>` + coverItem + `</manifest>
<spine><itemref idref="ch1"/><itemref idref="empty"/><itemref idref="ch2" linear="no"/></spine></package>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	w.Write([]byte("application/epub+zip"))
	add := func(name, data string) {
		w, _ := zw.Create(name)
		w.Write([]byte(data))
	}
	add("META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)
	add("OEBPS/content.opf", opf)
	add("OEBPS/ch1.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>First</title></head><body><h1>Chapter One</h1><p>Hello <b>epub</b> world.</p><img src="img/cover.png" alt=""/></body></html>`)
	add("OEBPS/ch2.xhtml", `<html><body><p>Not linear</p></body></html>`)
	add("OEBPS/empty.xhtml", `<html><body></body></html>`)
	if withCover {
		w, _ := zw.Create("OEBPS/img/cover.png")
		w.Write(pngBytes)
	}
	zw.Close()
	return buf.Bytes()
}

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewAndHealth(t *testing.T) {
	if New("") != nil {
		t.Error("empty root must yield a nil library")
	}
	var nilLib *Library
	if h := nilLib.Health(); h.Exists || h.Entries != 0 || h.Root != "" {
		t.Errorf("nil health: %+v", h)
	}
	if nilLib.HasArchive("a.zip") {
		t.Error("nil library has no archives")
	}
	root := t.TempDir()
	missing := New(filepath.Join(root, "nope"))
	if h := missing.Health(); h.Exists || h.Root == "" {
		t.Errorf("missing root: %+v", h)
	}
	empty := New(root)
	if h := empty.Health(); !h.Exists || h.Entries != 0 {
		t.Errorf("empty root: %+v", h)
	}
	os.WriteFile(filepath.Join(root, "a.zip"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	if h := empty.Health(); h.Entries != 2 {
		t.Errorf("entries: %+v", h)
	}
}

func TestHasArchive(t *testing.T) {
	root := t.TempDir()
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{"1.fb2": []byte("x")})
	copyFixture(t, "books.7z", filepath.Join(root, "b.7z"))
	os.Mkdir(filepath.Join(root, "loose"), 0o755)
	l := New(root)
	cases := map[string]bool{
		"a.zip": true, "a.7z": true, "a": true, // any spelling of an existing archive
		"b.7z": true, "b.zip": true,
		"loose": true,
		"c.zip": false, "../a.zip": false,
	}
	for folder, want := range cases {
		if got := l.HasArchive(folder); got != want {
			t.Errorf("HasArchive(%q) = %v, want %v", folder, got, want)
		}
	}
}

func TestOpen(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	writeZip(t, filepath.Join(root, "arch.zip"), map[string][]byte{
		"10.fb2": []byte("book ten"),
		"11":     []byte("book eleven, id only"),
		"12.jpg": []byte("not a book"),
		"README": []byte("x"),
	})
	os.Mkdir(filepath.Join(root, "loose"), 0o755)
	os.WriteFile(filepath.Join(root, "loose", "20.txt"), []byte("loose text"), 0o644)
	os.WriteFile(filepath.Join(root, "broken.zip"), []byte("this is not a zip"), 0o644)
	copyFixture(t, "books.7z", filepath.Join(root, "seven.7z"))

	get := func(folder, file, ext string) (string, int64, error) {
		rc, size, err := l.Open(folder, file, ext)
		if err != nil {
			return "", 0, err
		}
		return readAll(t, rc), size, nil
	}

	if s, size, err := get("arch.zip", "10", "fb2"); err != nil || s != "book ten" || size != 8 {
		t.Errorf("zip exact: %q %d %v", s, size, err)
	}
	if s, _, err := get("arch.zip", "10", "FB2"); err != nil || s != "book ten" {
		t.Errorf("zip case-insensitive: %q %v", s, err)
	}
	if s, _, err := get("arch.zip", "11", "fb2"); err != nil || s != "book eleven, id only" {
		t.Errorf("zip extensionless entry: %q %v", s, err)
	}
	if _, _, err := get("arch.zip", "12", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("a cover must not be mistaken for the book: %v", err)
	}
	if _, _, err := get("arch.zip", "99", "fb2"); !errors.Is(err, ErrNoFile) || !strings.Contains(err.Error(), "not found among 4 entries") {
		t.Errorf("missing entry diagnostics: %v", err)
	}
	// inpx says .7z but the disk has .zip, and vice versa.
	if s, _, err := get("arch.7z", "10", "fb2"); err != nil || s != "book ten" {
		t.Errorf(".7z name → .zip on disk: %q %v", s, err)
	}
	if s, _, err := get("seven.zip", "1", "fb2"); err != nil || !strings.Contains(s, "внутри 7z") {
		t.Errorf(".zip name → .7z on disk: %q %v", s, err)
	}
	if s, _, err := get("seven", "2", "fb2"); err != nil || !strings.Contains(s, "без расширения") {
		t.Errorf("no extension in inpx, id-only entry in 7z: %q %v", s, err)
	}
	if _, _, err := get("seven.7z", "3", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("7z missing entry: %v", err)
	}
	if s, size, err := get("loose", "20", "txt"); err != nil || s != "loose text" || size != 10 {
		t.Errorf("folder: %q %d %v", s, size, err)
	}
	if _, _, err := get("loose", "21", "txt"); !errors.Is(err, ErrNoFile) {
		t.Errorf("folder missing file: %v", err)
	}
	if _, _, err := get("nope.zip", "1", "fb2"); !errors.Is(err, ErrNoFile) || !strings.Contains(err.Error(), "not on disk") {
		t.Errorf("missing archive: %v", err)
	}
	if _, _, err := get("broken.zip", "1", "fb2"); !errors.Is(err, ErrNoFile) || !strings.Contains(err.Error(), "not a valid zip") {
		t.Errorf("broken archive: %v", err)
	}
	// Catalog metadata must not escape the root.
	for _, c := range [][3]string{{"../arch.zip", "10", "fb2"}, {"arch.zip", "../10", "fb2"}, {"arch.zip", "10", "fb2/../x"}} {
		if _, _, err := get(c[0], c[1], c[2]); !errors.Is(err, ErrNoFile) {
			t.Errorf("traversal %v: %v", c, err)
		}
	}
}

func TestOpenEPUBUnwrapsSevenZip(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	packed, _ := os.ReadFile(filepath.Join("testdata", "epub.7z"))
	plain := epubZip(t, false, true)
	writeZip(t, filepath.Join(root, "e.zip"), map[string][]byte{"1.epub": packed, "2.epub": plain, "3.epub": []byte("garbage")})

	rc, size, err := l.Open("e.zip", "1", "epub")
	if err != nil {
		t.Fatal(err)
	}
	data := readAll(t, rc)
	if int64(len(data)) != size {
		t.Errorf("size %d vs %d bytes", size, len(data))
	}
	zr, err := zip.NewReader(strings.NewReader(data), size)
	if err != nil {
		t.Fatalf("unwrapped epub is not a zip: %v", err)
	}
	if zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Errorf("mimetype must be first and stored: %s %d", zr.File[0].Name, zr.File[0].Method)
	}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "mybook/") {
			t.Errorf("folder prefix not stripped: %s", f.Name)
		}
	}
	cover, _, err := EPUBCover(strings.NewReader(data), size)
	if err != nil || len(cover) != len(pngBytes) {
		t.Errorf("cover of the repacked epub: %d %v", len(cover), err)
	}
	if text, err := l.Text("e.zip", "1", "epub", nil); err != nil || !strings.Contains(text.Chapters[0].HTML, "seven zip") {
		t.Errorf("text of the repacked epub: %+v %v", text, err)
	}

	// A regular zip-epub passes through untouched.
	rc, _, _ = l.Open("e.zip", "2", "epub")
	if got := readAll(t, rc); got != string(plain) {
		t.Error("plain epub must be returned as is")
	}
	// Garbage is neither 7z nor zip: returned as is, callers fail later.
	rc, _, _ = l.Open("e.zip", "3", "epub")
	if got := readAll(t, rc); got != "garbage" {
		t.Errorf("garbage epub: %q", got)
	}
	if _, err := l.Text("e.zip", "3", "epub", nil); err == nil {
		t.Error("garbage epub must not parse")
	}
}

func TestCover(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	// No <coverpage> and no binary that could pass for one.
	noCover := `<?xml version="1.0" encoding="UTF-8"?><FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description><title-info><book-title>Без обложки</book-title></title-info></description><body><section><p>Текст.</p></section></body></FictionBook>`
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{
		"1.fb2": []byte(testFB2), "2.fb2": []byte(noCover), "3.txt": []byte("plain"),
		"4.epub": epubZip(t, false, true), "5.epub": epubZip(t, true, true), "6.epub": epubZip(t, false, false),
	})

	// Embedded FB2 cover.
	data, mime, err := l.Cover(1, "a.zip", "1", "fb2")
	if err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Errorf("fb2 cover: %d %q %v", len(data), mime, err)
	}
	// Positive cache: the archive may disappear, the cover stays.
	os.Rename(filepath.Join(root, "a.zip"), filepath.Join(root, "a.bak"))
	if _, _, err := l.Cover(1, "a.zip", "1", "fb2"); err != nil {
		t.Errorf("cached cover: %v", err)
	}
	// Negative cache: a miss is remembered too.
	if _, _, err := l.Cover(2, "a.zip", "2", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("missing archive: %v", err)
	}
	os.Rename(filepath.Join(root, "a.bak"), filepath.Join(root, "a.zip"))
	if _, _, err := l.Cover(2, "a.zip", "2", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("negative cache must stick: %v", err)
	}
	if _, _, err := l.Cover(22, "a.zip", "2", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("fb2 without cover: %v", err)
	}
	if _, _, err := l.Cover(3, "a.zip", "3", "txt"); !errors.Is(err, ErrNoFile) {
		t.Errorf("txt has no cover: %v", err)
	}
	// EPUB3 property and EPUB2 meta pointer; epub without a cover.
	for _, c := range []struct {
		id   int64
		file string
		ok   bool
	}{{4, "4", true}, {5, "5", true}, {6, "6", false}} {
		data, mime, err := l.Cover(c.id, "a.zip", c.file, "epub")
		if c.ok && (err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes)) {
			t.Errorf("epub %s cover: %q %v", c.file, mime, err)
		}
		if !c.ok && !errors.Is(err, ErrNoFile) {
			t.Errorf("epub %s without cover: %v", c.file, err)
		}
	}

	// Sidecar covers win over embedded ones: covers/<archive>.zip, entries
	// named by book id with or without an extension.
	jpeg := []byte("\xFF\xD8\xFF\xE0jpegdata")
	writeZip(t, filepath.Join(root, "covers", "a.zip"), map[string][]byte{"1": jpeg, "2.png": pngBytes})
	fresh := New(root)
	if data, mime, err := fresh.Cover(1, "a.zip", "1", "fb2"); err != nil || mime != "image/jpeg" || !bytes.Equal(data, jpeg) {
		t.Errorf("sidecar (sniffed jpeg): %q %v", mime, err)
	}
	if data, mime, err := fresh.Cover(2, "a.zip", "2", "fb2"); err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Errorf("sidecar (.png): %q %v", mime, err)
	}
	// Sidecar in 7z, looked up by the inpx (.zip) name of the archive.
	copyFixture(t, "covers.7z", filepath.Join(root, "covers", "b.7z"))
	copyFixture(t, "books.7z", filepath.Join(root, "b.7z"))
	if data, mime, err := fresh.Cover(100, "b.zip", "1", "fb2"); err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Errorf("sidecar 7z: %d %q %v", len(data), mime, err)
	}
	if _, _, err := fresh.Cover(101, "b.zip", "2", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("no sidecar, no embedded cover: %v", err)
	}
	if _, _, err := fresh.Cover(102, "", "1", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("empty folder: %v", err)
	}

	// The cache is bounded: after coverCacheSize entries it starts over.
	for i := int64(1000); i < 1000+coverCacheSize+1; i++ {
		fresh.Cover(i, "missing.zip", "1", "fb2")
	}
	fresh.mu.Lock()
	n := len(fresh.covers)
	fresh.mu.Unlock()
	if n > coverCacheSize {
		t.Errorf("cover cache grew to %d", n)
	}
}

func TestSniffImageMime(t *testing.T) {
	cases := map[string]string{
		"\xFF\xD8\xFF\xE0....":                     "image/jpeg",
		"\x89PNG\r\n\x1a\n....":                    "image/png",
		"GIF89a....":                               "image/gif",
		"RIFF....WEBP....":                         "image/webp",
		"\xFF\x0Acodestream":                       "image/jxl",
		"\x00\x00\x00\x0CJXL \x0D\x0A\x87\x0A....": "image/jxl",
		"who knows":                                "image/jpeg",
		"":                                         "image/jpeg",
	}
	for in, want := range cases {
		if got := sniffImageMime([]byte(in)); got != want {
			t.Errorf("sniff(%q) = %q, want %q", in, got, want)
		}
	}
	// A non-JXL cover passes normalizeCover untouched; a JXL signature with
	// no valid stream falls back to the original bytes.
	if d, m := normalizeCover(pngBytes, "image/png"); m != "image/png" || !bytes.Equal(d, pngBytes) {
		t.Error("png must pass through")
	}
	bad := []byte("\xFF\x0Abroken")
	if d, m := normalizeCover(bad, "image/jxl"); m != "image/jxl" || !bytes.Equal(d, bad) {
		t.Error("undecodable jxl must be returned as is")
	}
}

func TestTextBinaryMeta(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{
		"1.fb2": []byte(testFB2), "2.txt": []byte("Глава первая\n\nТекст.\n"), "3.epub": epubZip(t, false, true),
		"4.pdf": []byte("%PDF"),
	})
	imgURL := func(id string) string { return "/img/" + id }

	text, err := l.Text("a.zip", "1", "fb2", imgURL)
	if err != nil || len(text.Chapters) == 0 || !strings.Contains(text.Chapters[0].HTML, "Текст книги") {
		t.Errorf("fb2 text: %+v %v", text, err)
	}
	text, err = l.Text("a.zip", "2", "txt", imgURL)
	if err != nil || len(text.Chapters) == 0 || !strings.Contains(text.Chapters[0].HTML, "Текст") {
		t.Errorf("txt text: %+v %v", text, err)
	}
	text, err = l.Text("a.zip", "3", "epub", imgURL)
	if err != nil {
		t.Fatalf("epub text: %v", err)
	}
	if len(text.Chapters) != 1 || text.Chapters[0].Title != "Chapter One" ||
		!strings.Contains(text.Chapters[0].HTML, "<b>epub</b>") || !strings.Contains(text.Chapters[0].HTML, `src="/img/OEBPS%2Fimg%2Fcover.png" width="1" height="1"`) {
		t.Errorf("epub chapters (empty and non-linear skipped, images rewritten): %+v", text.Chapters)
	}
	if _, err := l.Text("a.zip", "4", "pdf", imgURL); !errors.Is(err, ErrNoFile) {
		t.Errorf("pdf has no text: %v", err)
	}
	if _, err := l.Text("a.zip", "9", "fb2", imgURL); !errors.Is(err, ErrNoFile) {
		t.Errorf("missing book: %v", err)
	}

	data, mime, err := l.Binary("a.zip", "1", "fb2", "other.png")
	if err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Errorf("fb2 binary: %q %v", mime, err)
	}
	if _, _, err := l.Binary("a.zip", "1", "fb2", "nope.png"); err == nil {
		t.Error("unknown fb2 binary must fail")
	}
	data, mime, err = l.Binary("a.zip", "3", "epub", "OEBPS/img/cover.png")
	if err != nil || !strings.HasPrefix(mime, "image/png") || !bytes.Equal(data, pngBytes) {
		t.Errorf("epub binary: %q %v", mime, err)
	}
	if _, _, err := l.Binary("a.zip", "2", "txt", "x"); !errors.Is(err, ErrNoFile) {
		t.Errorf("txt has no binaries: %v", err)
	}

	meta, err := l.Meta("a.zip", "1", "fb2")
	if err != nil || meta.Title != "Тестовая книга" || len(meta.Cover) != 0 {
		t.Errorf("meta (without cover): %+v %v", meta, err)
	}
	if _, err := l.Meta("a.zip", "3", "epub"); !errors.Is(err, ErrNoFile) {
		t.Errorf("meta of non-fb2: %v", err)
	}
	if _, err := l.Meta("a.zip", "9", "fb2"); !errors.Is(err, ErrNoFile) {
		t.Errorf("meta of missing book: %v", err)
	}
}

func TestParseEPUBFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.epub")
	os.WriteFile(path, epubZip(t, false, true), 0o644)
	meta, err := ParseEPUBFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Test Epub" || len(meta.Authors) != 1 || meta.Authors[0] != "Ann Author" ||
		meta.Language != "en" || len(meta.Subjects) != 1 {
		t.Errorf("meta: %+v", meta)
	}
	if got := GenresFromSubjects(meta.Subjects); len(got) == 0 || got[0] != "sf" {
		t.Errorf("genres: %v", got)
	}
	os.WriteFile(path, []byte("nope"), 0o644)
	if _, err := ParseEPUBFile(path); err == nil {
		t.Error("not a zip must fail")
	}
	writeZip(t, path, map[string][]byte{"mimetype": []byte("application/epub+zip")})
	if _, err := ParseEPUBFile(path); err == nil || !strings.Contains(err.Error(), "container.xml") {
		t.Errorf("missing container: %v", err)
	}
	if _, _, err := EPUBCover(bytes.NewReader([]byte("nope")), 4); err == nil {
		t.Error("EPUBCover on garbage must fail")
	}
}

func TestArchiveCandidates(t *testing.T) {
	cases := map[string][]string{
		"/lib/a.zip": {"/lib/a.zip", "/lib/a.7z"},
		"/lib/a.7z":  {"/lib/a.7z", "/lib/a.zip"},
		"/lib/a":     {"/lib/a", "/lib/a.7z", "/lib/a.zip"},
		"/lib/a.ZIP": {"/lib/a.ZIP", "/lib/a.7z", "/lib/a.zip"},
	}
	for in, want := range cases {
		got := archiveCandidates(in)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("archiveCandidates(%q) = %v, want %v", in, got, want)
		}
	}
	if entryStem("794546.fb2") != "794546" || entryStem("794546") != "794546" || entryStem("a.b.c") != "a.b" {
		t.Error("entryStem")
	}
	err := entryNotFound("x", []string{"1", "2", "3", "4", "5", "6", "7"})
	if !strings.Contains(err.Error(), "among 7 entries") || strings.Contains(err.Error(), "6") {
		t.Errorf("entryNotFound sample: %v", err)
	}
}
