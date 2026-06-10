package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUploadDuplicates(t *testing.T) {
	ts, client, _ := newManageServer(t)

	upload := func(name string, body []byte, force bool) uploadResult {
		fields := map[string]string{}
		if force {
			fields["force"] = "1"
		}
		resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{name: body}, fields)
		var up map[string][]uploadResult
		json.NewDecoder(resp.Body).Decode(&up)
		resp.Body.Close()
		if len(up["results"]) != 1 {
			t.Fatalf("results = %v", up)
		}
		return up["results"][0]
	}

	// First upload — success
	first := upload("voyna.fb2", []byte(opdsFB2), false)
	if first.BookID == 0 || first.Error != "" {
		t.Fatalf("first upload: %+v", first)
	}

	// Level 1: the same file under a different name — exact copy
	dup := upload("voyna-copy.fb2", []byte(opdsFB2), false)
	if dup.Error == "" || dup.Duplicate == nil || dup.Duplicate.Reason != "file" {
		t.Errorf("exact dup: %+v", dup)
	}
	if dup.Duplicate != nil && dup.Duplicate.BookID != first.BookID {
		t.Errorf("dup points to %d, want %d", dup.Duplicate.BookID, first.BookID)
	}

	// Level 2: same text, modified metadata (different title tag)
	changedMeta := strings.Replace(opdsFB2, "<book-title>Война и мир</book-title>",
		"<book-title>Война и мир. Издание второе</book-title>", 1)
	dup = upload("voyna-2e.fb2", []byte(changedMeta), false)
	if dup.Error == "" || dup.Duplicate == nil || dup.Duplicate.Reason != "content" {
		t.Errorf("content dup: %+v", dup)
	}

	// Level 3: same title and author, different text — a warning
	otherText := strings.Replace(opdsFB2, "<p>Текст.</p>", "<p>Совсем другой текст книги.</p>", 1)
	warn := upload("voyna-other.fb2", []byte(otherText), false)
	if !warn.NeedConfirm || warn.Duplicate == nil || warn.Duplicate.Reason != "metadata" {
		t.Fatalf("metadata warn: %+v", warn)
	}
	if warn.BookID != 0 {
		t.Error("book must not be added without confirmation")
	}

	// force=1 — it uploads
	forced := upload("voyna-other.fb2", []byte(otherText), true)
	if forced.BookID == 0 || forced.Error != "" {
		t.Errorf("forced upload: %+v", forced)
	}

	// Different books are unaffected
	other := strings.NewReplacer(
		"Война и мир", "Анна Каренина",
		"<p>Текст.</p>", "<p>Все счастливые семьи похожи друг на друга.</p>",
	).Replace(opdsFB2)
	ok := upload("anna.fb2", []byte(other), false)
	if ok.BookID == 0 || ok.Error != "" || ok.NeedConfirm {
		t.Errorf("different book: %+v", ok)
	}
}
