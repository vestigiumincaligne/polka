package server

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	fb2HrefPattern    = regexp.MustCompile(`(?i)((?:xlink:)?href\s*=\s*["'])#([^"']+)(["'])`)
	fb2SectionPattern = regexp.MustCompile(`(?i)<section(?:\s[^<>]*?)?>`)
	fb2IDPattern      = regexp.MustCompile(`(?i)\sid\s*=\s*["'][^"']+["']`)
)

// handleBookEpub converts an FB2 book on first request. It reads the source
// through Library.Open and writes only to DataDir/epub-cache; the library
// (including a mounted Flibusta archive) is never modified.
func (s *Server) handleBookEpub(w http.ResponseWriter, r *http.Request) {
	f := s.bookFileOr404(w, r)
	if f == nil {
		return
	}
	if !strings.EqualFold(f.Ext, "fb2") {
		http.NotFound(w, r)
		return
	}
	if _, err := exec.LookPath("pandoc"); err != nil {
		s.log.Error("epub conversion", "book", f.ID, "error", "pandoc not found")
		http.Error(w, "pandoc is not installed", http.StatusInternalServerError)
		return
	}

	// Keep generated formats versioned so a preprocessing fix never serves an
	// older cached EPUB with broken navigation or resources.
	cacheDir := filepath.Join(s.cfg.DataDir, "epub-cache", "v2")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		s.apiError(w, err)
		return
	}
	cachePath := filepath.Join(cacheDir, strconv.FormatInt(f.ID, 10)+".epub")
	if validRegularFile(cachePath) {
		serveEpub(w, r, cachePath, f.File)
		return
	}

	rc, _, err := s.lib.Open(f.Folder, f.File, f.Ext)
	if err != nil {
		s.apiError(w, err)
		return
	}
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		s.apiError(w, readErr)
		return
	}
	if closeErr != nil {
		s.apiError(w, closeErr)
		return
	}

	tmpDir, err := os.MkdirTemp(cacheDir, ".convert-*")
	if err != nil {
		s.apiError(w, err)
		return
	}
	defer os.RemoveAll(tmpDir)

	inputPath, err := prepareFB2ForPandoc(data, tmpDir)
	if err != nil {
		s.log.Warn("epub preprocess", "book", f.ID, "error", err)
		http.Error(w, "invalid FB2", http.StatusUnprocessableEntity)
		return
	}
	tmpEpub := filepath.Join(tmpDir, "book.epub")
	cmd := exec.CommandContext(r.Context(), "pandoc", inputPath, "--from=fb2", "--to=epub3", "-o", tmpEpub)
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.log.Error("epub conversion", "book", f.ID, "error", err, "output", strings.TrimSpace(string(output)))
		http.Error(w, "epub conversion failed", http.StatusInternalServerError)
		return
	}
	if !validRegularFile(tmpEpub) {
		http.Error(w, "epub conversion produced no output", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpEpub, cachePath); err != nil {
		s.apiError(w, err)
		return
	}
	s.log.Info("epub created", "book", f.ID, "path", cachePath)
	serveEpub(w, r, cachePath, f.File)
}

func validRegularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func serveEpub(w http.ResponseWriter, r *http.Request, path, baseName string) {
	name := baseName + ".epub"
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	w.Header().Set("Content-Type", "application/epub+zip")
	w.Header().Set("Content-Disposition", disposition)
	http.ServeFile(w, r, path)
}

// prepareFB2ForPandoc extracts embedded <binary> payloads beside the temporary
// FB2 and rewrites local #image references. All generated paths are confined to
// dir even if catalog/XML metadata contains path separators.
func prepareFB2ForPandoc(data []byte, dir string) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	names := make(map[string]string)
	used := make(map[string]string)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse fb2: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "binary") {
			continue
		}
		var id string
		for _, attr := range start.Attr {
			if strings.EqualFold(attr.Name.Local, "id") {
				id = attr.Value
				break
			}
		}
		var payload string
		if err := dec.DecodeElement(&payload, &start); err != nil {
			return "", fmt.Errorf("decode binary element: %w", err)
		}
		if id == "" {
			continue
		}
		name := filepath.Base(strings.ReplaceAll(id, "\\", "/"))
		if name == "" || name == "." || name == ".." {
			return "", fmt.Errorf("invalid embedded filename %q", id)
		}
		if previous, exists := used[name]; exists && previous != id {
			return "", fmt.Errorf("embedded filename collision: %q and %q", previous, id)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
		if err != nil {
			return "", fmt.Errorf("decode embedded file %q: %w", id, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			return "", fmt.Errorf("write embedded file %q: %w", name, err)
		}
		names[id] = name
		used[name] = id
	}

	fixed := fb2HrefPattern.ReplaceAllStringFunc(string(data), func(match string) string {
		parts := fb2HrefPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		name, ok := names[parts[2]]
		if !ok {
			return match
		}
		return parts[1] + name + parts[3]
	})
	sectionNumber := 0
	fixed = fb2SectionPattern.ReplaceAllStringFunc(fixed, func(tag string) string {
		sectionNumber++
		if fb2IDPattern.MatchString(tag) {
			return tag
		}
		return tag[:len(tag)-1] + fmt.Sprintf(` id="polka-section-%d">`, sectionNumber)
	})
	path := filepath.Join(dir, "book.fb2")
	if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
		return "", fmt.Errorf("write temporary fb2: %w", err)
	}
	return path, nil
}
