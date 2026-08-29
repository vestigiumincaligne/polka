package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vestigiumincaligne/polka/internal/importer"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
)

const (
	uploadsFolder     = "uploads"
	maxUploadSize     = 512 << 20 // total size of a single upload
	maxExportBooks    = 500
	uploadFilePerms   = 0o644
	uploadFolderPerms = 0o755
)

var supportedUploadExt = map[string]bool{
	"fb2": true, "epub": true, "pdf": true, "djvu": true, "txt": true, "mobi": true, "azw3": true,
}

// --- Book upload ---

type duplicateInfo struct {
	BookID  int64  `json:"bookId"`
	Title   string `json:"title"`
	Authors string `json:"authors,omitempty"`
	Reason  string `json:"reason"` // file | content | metadata
}

type uploadResult struct {
	Name        string         `json:"name"`
	BookID      int64          `json:"bookId,omitempty"`
	Title       string         `json:"title,omitempty"`
	Authors     string         `json:"authors,omitempty"`
	Error       string         `json:"error,omitempty"`
	Duplicate   *duplicateInfo `json:"duplicate,omitempty"`
	NeedConfirm bool           `json:"needConfirm,omitempty"` // metadata match: can be uploaded with force=1
}

func (s *Server) handleBookUpload(w http.ResponseWriter, r *http.Request) {
	if s.lib == nil {
		http.Error(w, "library-dir is not configured", http.StatusConflict)
		return
	}
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "upload too large or malformed", http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files", http.StatusBadRequest)
		return
	}

	uploadDir := filepath.Join(s.cfg.LibraryDir, uploadsFolder)
	if err := os.MkdirAll(uploadDir, uploadFolderPerms); err != nil {
		s.apiError(w, err)
		return
	}

	force := r.FormValue("force") == "1"
	lang := reqLang(r)
	results := make([]uploadResult, 0, len(files))
	for _, fh := range files {
		res := s.saveUploadedBook(r.Context(), uploadDir, fh, force, lang)
		results = append(results, res)
	}
	writeJSON(w, map[string]any{"results": results})
}

func (s *Server) saveUploadedBook(ctx context.Context, uploadDir string, fh *multipart.FileHeader, force bool, lang string) uploadResult {
	res := uploadResult{Name: fh.Filename}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fh.Filename), "."))
	if !supportedUploadExt[ext] {
		res.Error = tr(lang, "upload.badformat")
		return res
	}

	base := sanitizeFileName(strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename)))
	if base == "" {
		base = "book"
	}
	dst, base, err := createUnique(uploadDir, base, ext)
	if err != nil {
		res.Error = tr(lang, "upload.savefail")
		s.log.Error("upload", "file", fh.Filename, "error", err)
		return res
	}

	src, err := fh.Open()
	if err != nil {
		dst.Close()
		res.Error = tr(lang, "upload.readfail")
		return res
	}
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(dst, hasher), src)
	src.Close()
	dst.Close()
	if err != nil {
		os.Remove(dst.Name())
		res.Error = tr(lang, "upload.savefail")
		return res
	}
	fileHash := hex.EncodeToString(hasher.Sum(nil))

	discard := func() { os.Remove(dst.Name()) }

	// Level 1: exact copy of the file.
	if dup, err := s.st.FindByHash(ctx, "file_hash", fileHash); err == nil {
		discard()
		res.Error = tr(lang, "upload.dup.file")
		res.Duplicate = &duplicateInfo{BookID: dup.ID, Title: dup.Title, Authors: dup.AuthorNames, Reason: "file"}
		return res
	}

	// Level 2 (fb2): same text with different metadata/encoding.
	var contentHash string
	if ext == "fb2" {
		if f, err := os.Open(dst.Name()); err == nil {
			contentHash, _ = library.ContentHash(f)
			f.Close()
		}
		if dup, err := s.st.FindByHash(ctx, "content_hash", contentHash); err == nil {
			discard()
			res.Error = tr(lang, "upload.dup.text")
			res.Duplicate = &duplicateInfo{BookID: dup.ID, Title: dup.Title, Authors: dup.AuthorNames, Reason: "content"}
			return res
		}
	}

	input := s.extractUploadMeta(dst.Name(), base, ext)
	input.Folder = uploadsFolder
	input.File = base
	input.Ext = ext
	input.Size = size
	input.Added = time.Now().Format("2006-01-02")
	input.FileHash = fileHash
	input.ContentHash = contentHash

	// Level 3: metadata match — warn, but allow the upload.
	if !force {
		var lastNames []string
		for _, a := range input.Authors {
			lastNames = append(lastNames, a.Last)
		}
		if similar, err := s.st.FindSimilar(ctx, input.Title, lastNames); err == nil && len(similar) > 0 {
			discard()
			res.NeedConfirm = true
			res.Title = input.Title
			res.Duplicate = &duplicateInfo{
				BookID: similar[0].ID, Title: similar[0].Title,
				Authors: similar[0].AuthorNames, Reason: "metadata",
			}
			return res
		}
	}

	bookID, err := s.st.AddBook(ctx, input)
	if err != nil {
		os.Remove(dst.Name())
		res.Error = tr(lang, "upload.dbfail")
		s.log.Error("upload add", "file", fh.Filename, "error", err)
		return res
	}

	res.BookID = bookID
	res.Title = input.Title
	names := make([]string, 0, len(input.Authors))
	for _, a := range input.Authors {
		names = append(names, strings.TrimSpace(a.Last+" "+a.First))
	}
	res.Authors = strings.Join(names, ", ")
	s.log.Info("book uploaded", "id", bookID, "title", input.Title, "file", base+"."+ext)
	return res
}

// extractUploadMeta extracts metadata from the file; on failure
// the title is taken from the file name.
func (s *Server) extractUploadMeta(path, base, ext string) *store.BookInput {
	input := &store.BookInput{Title: base}

	switch ext {
	case "fb2":
		f, err := os.Open(path)
		if err != nil {
			return input
		}
		defer f.Close()
		meta, err := library.ParseFB2(f, false)
		if err != nil {
			s.log.Warn("upload fb2 meta", "file", base, "error", err)
			return input
		}
		if meta.Title != "" {
			input.Title = meta.Title
		}
		for _, a := range meta.Authors {
			input.Authors = append(input.Authors, store.AuthorName{Last: a.Last, First: a.First, Middle: a.Middle})
		}
		input.Series = meta.Series
		input.SeriesNum = meta.SeriesNum
		input.Genres = meta.Genres
		input.Lang = meta.Lang
		input.ISBN = meta.ISBN

	case "epub":
		meta, err := library.ParseEPUBFile(path)
		if err != nil {
			s.log.Warn("upload epub meta", "file", base, "error", err)
			return input
		}
		if meta.Title != "" {
			input.Title = meta.Title
		}
		for _, name := range meta.Authors {
			input.Authors = append(input.Authors, store.AuthorName{Last: name})
		}
		input.Lang = meta.Language
		input.Keywords = meta.Subjects
		input.Genres = library.GenresFromSubjects(meta.Subjects)
	}
	return input
}

func sanitizeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

// createUnique creates file base.ext; on collision base_2.ext and so on.
func createUnique(dir, base, ext string) (*os.File, string, error) {
	name := base
	for i := 2; ; i++ {
		f, err := os.OpenFile(filepath.Join(dir, name+"."+ext), os.O_CREATE|os.O_EXCL|os.O_WRONLY, uploadFilePerms)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
		if i > 1000 {
			return nil, "", errors.New("too many name collisions")
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
}

// --- Deletion/restoration ---

func (s *Server) handleBookSetDeleted(deleted bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := s.st.SetBookDeleted(r.Context(), id, deleted); err != nil {
			s.apiError(w, err)
			return
		}
		s.log.Info("book visibility changed", "id", id, "deleted", deleted)
		writeJSON(w, map[string]any{"ok": true})
	}
}

// --- Bulk export ---

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if s.lib == nil {
		http.Error(w, "library dir is not configured", http.StatusNotFound)
		return
	}
	var ids []int64
	for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > maxExportBooks {
		http.Error(w, fmt.Sprintf("ids: from 1 to %d book ids", maxExportBooks), http.StatusBadRequest)
		return
	}

	files, err := s.st.BookFilesByIDs(r.Context(), ids)
	if err != nil {
		s.apiError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="polka-books.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()
	used := map[string]int{}
	for _, f := range files {
		rc, _, err := s.lib.Open(f.Folder, f.File, f.Ext)
		if err != nil {
			s.log.Warn("export: skip missing", "book", f.ID, "error", err)
			continue
		}
		name := sanitizeFileName(f.Title)
		if name == "" {
			name = f.File
		}
		if used[name]++; used[name] > 1 {
			name = fmt.Sprintf("%s_%d", name, used[name])
		}
		entry, err := zw.Create(name + "." + f.Ext)
		if err == nil {
			_, err = io.Copy(entry, rc)
		}
		rc.Close()
		if err != nil {
			s.log.Warn("export stream", "book", f.ID, "error", err)
			return
		}
	}
}

// --- inpx import via the web ---

type importState struct {
	mu        sync.Mutex
	running   bool
	phase     string
	processed int
	stats     store.ImportStats
	err       string
	warning   string // non-fatal, e.g. archives not found under the library dir
}

func (st *importState) snapshot() map[string]any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return map[string]any{
		"running":   st.running,
		"phase":     st.phase,
		"processed": st.processed,
		"books":     st.stats.Books,
		"authors":   st.stats.Authors,
		"series":    st.stats.Series,
		"error":     st.err,
		"warning":   st.warning,
	}
}

func (s *Server) handleImportStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.imp.snapshot())
}

func (s *Server) handleImportInpx(w http.ResponseWriter, r *http.Request) {
	s.imp.mu.Lock()
	if s.imp.running {
		s.imp.mu.Unlock()
		http.Error(w, "import is already running", http.StatusConflict)
		return
	}
	s.imp.running = true
	s.imp.phase = "starting"
	s.imp.processed = 0
	s.imp.err = ""
	s.imp.warning = ""
	s.imp.stats = store.ImportStats{}
	s.imp.mu.Unlock()

	fail := func(code int, msg string) {
		s.imp.mu.Lock()
		s.imp.running = false
		s.imp.phase = "error"
		s.imp.err = msg
		s.imp.mu.Unlock()
		http.Error(w, msg, code)
	}

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		fail(http.StatusBadRequest, "upload too large or malformed")
		return
	}

	replace := r.FormValue("replace") == "1"
	if n, err := s.st.BookCount(r.Context()); err != nil {
		fail(http.StatusInternalServerError, err.Error())
		return
	} else if n > 0 && !replace {
		fail(http.StatusConflict, "library is not empty; pass replace=1 to reimport")
		return
	}

	// Path mode: the inpx already lives on the server (e.g. a mounted
	// or NFS collection), so we import it in place without copying.
	if serverPath := strings.TrimSpace(r.FormValue("path")); serverPath != "" {
		fi, err := os.Stat(serverPath)
		if err != nil || fi.IsDir() {
			fail(http.StatusBadRequest, "inpx path not found on the server")
			return
		}
		go s.runImport(serverPath, replace, false)
		writeJSON(w, map[string]any{"started": true})
		return
	}

	// Upload mode: the inpx is sent as a multipart file.
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "inpx file or server path is required")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp(s.cfg.DataDir, "import-*.inpx")
	if err != nil {
		fail(http.StatusInternalServerError, "cannot save inpx")
		return
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		fail(http.StatusInternalServerError, "cannot save inpx")
		return
	}
	tmp.Close()

	go s.runImport(tmp.Name(), replace, true)
	writeJSON(w, map[string]any{"started": true})
}

func (s *Server) runImport(inpxPath string, replace bool, cleanup bool) {
	if cleanup {
		defer os.Remove(inpxPath) // remove the uploaded temp copy, not a server path
	}
	ctx := context.Background()

	setPhase := func(phase string, processed int) {
		s.imp.mu.Lock()
		s.imp.phase = phase
		if processed > 0 {
			s.imp.processed = processed
		}
		s.imp.mu.Unlock()
	}

	finish := func(stats store.ImportStats, err error) {
		s.imp.mu.Lock()
		defer s.imp.mu.Unlock()
		s.imp.running = false
		if err != nil {
			s.imp.phase = "error"
			s.imp.err = err.Error()
			s.log.Error("web import", "error", err)
			return
		}
		s.imp.phase = "done"
		s.imp.stats = stats
		s.log.Info("web import done", "books", stats.Books, "duration", stats.Duration)
	}

	if replace {
		setPhase("clearing", 0)
		if err := s.st.Clear(ctx); err != nil {
			finish(store.ImportStats{}, err)
			return
		}
	}

	stats, err := importer.ImportInpx(ctx, s.log, s.st, inpxPath, setPhase)
	finish(stats, err)
	if err == nil {
		s.warnIfArchivesMissing(ctx)
		s.rematchCollections(ctx)
	}
}

// warnIfArchivesMissing checks, on a sample of catalog folders, that the
// archives actually live under the library dir. A common mistake: the catalog
// is imported from an inpx (metadata only), but the archives are mounted at a
// different path than POLKA_LIBRARY_DIR — then books "won't open" and covers
// are missing.
func (s *Server) warnIfArchivesMissing(ctx context.Context) {
	if s.lib == nil {
		return
	}
	folders, err := s.st.SampleFolders(ctx, 50)
	if err != nil || len(folders) == 0 {
		return
	}
	missing := 0
	for _, f := range folders {
		if !s.lib.HasArchive(f) {
			missing++
		}
	}
	if missing == 0 {
		return
	}
	msg := fmt.Sprintf("catalog imported, but %d of %d sampled book archives were not found under the library directory %q — if your books are mounted elsewhere, set POLKA_LIBRARY_DIR to that path and restart",
		missing, len(folders), s.cfg.LibraryDir)
	s.log.Warn("library archives missing after import",
		"missing", missing, "sampled", len(folders), "library_dir", s.cfg.LibraryDir)
	s.imp.mu.Lock()
	s.imp.warning = msg
	s.imp.mu.Unlock()
}
