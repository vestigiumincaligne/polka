package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// newManageServer — a server with an empty library and an admin, already logged in.
func newManageServer(t *testing.T) (*httptest.Server, *http.Client, *store.Store) {
	t.Helper()
	ts, client, st, _ := newManageServerDir(t)
	return ts, client, st
}

// newManageServerDir is the same, plus a data directory (users.db, collections.db).
func newManageServerDir(t *testing.T) (*httptest.Server, *http.Client, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(libDir, 0o755)

	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	users, err := auth.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { users.Close() })
	users.CreateUser(context.Background(), "admin", "secret123", "", auth.RoleAdmin)

	cfg := &config.Config{LibraryDir: libDir, DataDir: dir, Auth: "required"}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html/>")}}
	srv := New(cfg, log, st, library.New(libDir), users, nil, webFS)

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp := postJSON(t, client, ts.URL+"/auth/login", map[string]string{"login": "admin", "password": "secret123"})
	resp.Body.Close()
	return ts, client, st, dir
}

func uploadFiles(t *testing.T, client *http.Client, url string, files map[string][]byte, fields map[string]string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, data := range files {
		w, _ := mw.CreateFormFile("files", name)
		w.Write(data)
	}
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestUploadDeleteExport(t *testing.T) {
	ts, client, _ := newManageServer(t)

	// Uploading an fb2 + an unsupported file
	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"Tolstoy - Voyna i mir.fb2": []byte(testFB2),
		"malware.exe":               []byte("nope"),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("upload -> %d", resp.StatusCode)
	}

	var bookID int64
	for _, r := range up["results"] {
		switch {
		case r.Name == "malware.exe" && r.Error == "":
			t.Error("exe must be rejected")
		case r.Name != "malware.exe":
			if r.Title != "Война и мир" {
				t.Errorf("fb2 meta not extracted: %+v", r)
			}
			bookID = r.BookID
		}
	}
	if bookID == 0 {
		t.Fatalf("fb2 not uploaded: %+v", up)
	}

	// The book is searchable
	var stats map[string]map[string]int
	resp, _ = client.Get(ts.URL + "/main/getBooks/getSearchStats?search=войн")
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["searchStats"]["bookTitles"] != 1 {
		t.Errorf("uploaded book not searchable: %v", stats)
	}

	// It downloads and shows up in the bulk export
	resp, _ = client.Get(ts.URL + "/Images/export?ids=" + itoa64(bookID))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) != 1 {
		t.Fatalf("export zip: %v, files=%d", err, len(zr.File))
	}
	if zr.File[0].Name != "Война и мир.fb2" {
		t.Errorf("export entry name = %q", zr.File[0].Name)
	}

	// Soft delete: disappears from search and the book card
	resp = postJSON(t, client, ts.URL+"/admin/books/"+itoa64(bookID)+"/delete", nil)
	resp.Body.Close()
	resp, _ = client.Get(ts.URL + "/main/getBooks/getBookForm?selectedItemID=" + itoa64(bookID))
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("deleted book form -> %d, want 404", resp.StatusCode)
	}
	resp, _ = client.Get(ts.URL + "/main/getBooks/getSearchStats?search=войн")
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["searchStats"]["bookTitles"] != 0 {
		t.Errorf("deleted book still searchable: %v", stats)
	}

	// Restoration
	resp = postJSON(t, client, ts.URL+"/admin/books/"+itoa64(bookID)+"/restore", nil)
	resp.Body.Close()
	resp, _ = client.Get(ts.URL + "/main/getBooks/getBookForm?selectedItemID=" + itoa64(bookID))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("restored book form -> %d", resp.StatusCode)
	}
}

func TestWebImportInpx(t *testing.T) {
	ts, client, _ := newManageServer(t)

	// Prepare a tiny inpx
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("books.inp")
	w.Write([]byte("Чехов,Антон,\x04prose_classic\x04Палата №6\x04\x04\x04ch1\x04500\x041\x040\x04fb2\x042024-01-01\x04ru\x045\x04\x041892\x04src\r\n"))
	zw.Close()

	var mbuf bytes.Buffer
	mw := multipart.NewWriter(&mbuf)
	fw, _ := mw.CreateFormFile("file", "small.inpx")
	fw.Write(buf.Bytes())
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/admin/import/inpx", &mbuf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("import start -> %d", resp.StatusCode)
	}

	// Wait for the background import to finish
	deadline := 100
	for ; deadline > 0; deadline-- {
		var status map[string]any
		resp, _ := client.Get(ts.URL + "/admin/import/status")
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status["running"] == false {
			if status["phase"] != "done" {
				t.Fatalf("import failed: %v", status)
			}
			if status["books"].(float64) != 1 {
				t.Errorf("imported books = %v", status["books"])
			}
			break
		}
	}
	if deadline == 0 {
		t.Fatal("import did not finish")
	}

	var stats map[string]map[string]int
	resp, _ = client.Get(ts.URL + "/main/getBooks/getSearchStats?search=палата")
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["searchStats"]["bookTitles"] != 1 {
		t.Errorf("imported book not searchable: %v", stats)
	}
}

func TestWebImportInpxFromPath(t *testing.T) {
	ts, client, _ := newManageServer(t)

	// Write a tiny inpx to a real file on disk (simulating a mounted /
	// NFS collection the admin points at instead of uploading).
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("books.inp")
	w.Write([]byte("Лесков,Николай,\x04prose_classic\x04Левша\x04\x04\x04lefty\x04400\x041\x040\x04fb2\x042024-01-01\x04ru\x045\x04\x041881\x04src\r\n"))
	zw.Close()

	dir := t.TempDir()
	inpxPath := filepath.Join(dir, "collection.inpx")
	if err := os.WriteFile(inpxPath, zbuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	post := func(fields map[string]string) int {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/admin/import/inpx", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := post(map[string]string{}); code != http.StatusBadRequest {
		t.Fatalf("empty import -> %d", code)
	}
	if code := post(map[string]string{"path": filepath.Join(dir, "nope.inpx")}); code != http.StatusBadRequest {
		t.Fatalf("bad path -> %d", code)
	}
	if code := post(map[string]string{"path": inpxPath}); code != http.StatusOK {
		t.Fatalf("path import start -> %d", code)
	}

	for deadline := 100; deadline > 0; deadline-- {
		var status map[string]any
		resp, _ := client.Get(ts.URL + "/admin/import/status")
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status["running"] == false {
			if status["phase"] != "done" {
				t.Fatalf("path import failed: %v", status)
			}
			break
		}
		if deadline == 1 {
			t.Fatal("path import did not finish")
		}
	}

	if _, err := os.Stat(inpxPath); err != nil {
		t.Errorf("server-path inpx was removed: %v", err)
	}
}
