package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareFB2ForPandocExtractsAndRewritesImage(t *testing.T) {
	dir := t.TempDir()
	source := `<?xml version="1.0" encoding="utf-8"?>
<FictionBook xmlns:xlink="http://www.w3.org/1999/xlink">
  <description><title-info><coverpage><image xlink:href="#cover.jpg"/></coverpage></title-info></description>
  <body><section><p>text</p></section></body>
  <binary id="cover.jpg" content-type="image/jpeg">aGVsbG8=</binary>
</FictionBook>`
	path, err := prepareFB2ForPandoc([]byte(source), dir)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixed), `xlink:href="cover.jpg"`) {
		t.Fatalf("image href was not rewritten: %s", fixed)
	}
	image, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(image) != "hello" {
		t.Fatalf("unexpected extracted image: %q", image)
	}
}

func TestPrepareFB2ForPandocKeepsEmbeddedFileInsideTempDir(t *testing.T) {
	dir := t.TempDir()
	source := `<FictionBook><binary id="../../cover.jpg">aGVsbG8=</binary></FictionBook>`
	if _, err := prepareFB2ForPandoc([]byte(source), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cover.jpg")); err != nil {
		t.Fatalf("safe basename was not written: %v", err)
	}
}

func TestPrepareFB2ForPandocAddsMissingSectionIDs(t *testing.T) {
	dir := t.TempDir()
	source := `<FictionBook><body><section><title><p>One</p></title></section>` +
		`<section id="existing"><title><p>Two</p></title></section>` +
		`<section><title><p>Three</p></title></section></body></FictionBook>`
	path, err := prepareFB2ForPandoc([]byte(source), dir)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(fixed)
	for _, want := range []string{`id="polka-section-1"`, `id="existing"`, `id="polka-section-3"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}
