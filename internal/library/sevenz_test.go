package library

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestOpen7zWithZipName(t *testing.T) {
	root := os.Getenv("ROOT")
	if root == "" {
		t.Skip("set ROOT to a dir containing the test .7z")
	}
	l := New(root)
	// inpx says ".zip" but disk has ".7z" — must still read.
	rc, size, err := l.Open("f.fb2-009373-367300.zip", "339380", "fb2")
	if err != nil {
		t.Fatalf("Open .7z via .zip name: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if size == 0 || !strings.Contains(string(data), "Текст книги внутри 7z") {
		t.Errorf("content not read from 7z (size=%d): %q", size, string(data)[:min(80, len(data))])
	}

	// Direct .7z name must also work.
	rc2, _, err := l.Open("f.fb2-009373-367300.7z", "339380", "fb2")
	if err != nil {
		t.Fatalf("Open .7z direct: %v", err)
	}
	rc2.Close()
}

func min(a, b int) int { if a < b { return a }; return b }

func TestSidecarCover(t *testing.T) {
	root := os.Getenv("ROOT")
	if root == "" {
		t.Skip("set ROOT")
	}
	l := New(root)
	// folder = архив книги (.zip-имя как в inpx); обложка в covers/<base>.zip
	data, mime, err := l.Cover(339380, "f.fb2-009373-367300.zip", "339380", "fb2")
	if err != nil {
		t.Fatalf("sidecar cover: %v", err)
	}
	if len(data) == 0 || mime != "image/jpeg" {
		t.Errorf("cover data=%d mime=%q", len(data), mime)
	}
	// первые байты — JPEG SOI
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Errorf("not a JPEG: % x", data[:min(4, len(data))])
	}
}
