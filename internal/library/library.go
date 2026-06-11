// Package library provides access to book files: archives (zip) and folders
// under the library root, plus extraction of covers and FB2 metadata.
package library

import (
	"bytes"
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrNoFile = errors.New("book file not found")

type Library struct {
	root string

	mu     sync.Mutex
	covers map[int64]coverEntry // bounded cover cache
}

type coverEntry struct {
	data []byte
	mime string
}

const coverCacheSize = 512

func New(root string) *Library {
	if root == "" {
		return nil
	}
	return &Library{root: root, covers: make(map[int64]coverEntry, coverCacheSize)}
}

// Open returns the contents of a book file: folder is an archive or a subfolder
// under the library root, the file name is file + "." + ext.
func (l *Library) Open(folder, file, ext string) (io.ReadCloser, int64, error) {
	name := file + "." + ext
	path := filepath.Join(l.root, filepath.FromSlash(folder))

	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		f, err := os.Open(filepath.Join(path, name))
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %s/%s", ErrNoFile, folder, name)
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, err
		}
		return f, fi.Size(), nil
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: open %s: %v", ErrNoFile, folder, err)
	}
	for _, entry := range zr.File {
		if strings.EqualFold(entry.Name, name) {
			rc, err := entry.Open()
			if err != nil {
				zr.Close()
				return nil, 0, err
			}
			return &zipEntryReader{rc: rc, zr: zr}, int64(entry.UncompressedSize64), nil
		}
	}
	zr.Close()
	return nil, 0, fmt.Errorf("%w: %s in %s", ErrNoFile, name, folder)
}

type zipEntryReader struct {
	rc io.ReadCloser
	zr *zip.ReadCloser
}

func (z *zipEntryReader) Read(p []byte) (int, error) { return z.rc.Read(p) }

func (z *zipEntryReader) Close() error {
	z.rc.Close()
	return z.zr.Close()
}

// Cover returns the book cover (for fb2), with an in-memory cache.
func (l *Library) Cover(bookID int64, folder, file, ext string) ([]byte, string, error) {
	l.mu.Lock()
	if c, ok := l.covers[bookID]; ok {
		l.mu.Unlock()
		if c.data == nil {
			return nil, "", ErrNoFile // negative cache
		}
		return c.data, c.mime, nil
	}
	l.mu.Unlock()

	data, mime, err := l.extractCover(folder, file, ext)

	l.mu.Lock()
	if len(l.covers) >= coverCacheSize {
		// simple eviction: drop the whole cache
		l.covers = make(map[int64]coverEntry, coverCacheSize)
	}
	l.covers[bookID] = coverEntry{data: data, mime: mime}
	l.mu.Unlock()

	if err != nil {
		return nil, "", err
	}
	return data, mime, nil
}

func (l *Library) extractCover(folder, file, ext string) ([]byte, string, error) {
	switch {
	case strings.EqualFold(ext, "fb2"):
		rc, _, err := l.Open(folder, file, ext)
		if err != nil {
			return nil, "", err
		}
		defer rc.Close()
		meta, err := ParseFB2(rc, true)
		if err != nil || len(meta.Cover) == 0 {
			return nil, "", ErrNoFile
		}
		return meta.Cover, meta.CoverMime, nil

	case strings.EqualFold(ext, "epub"):
		rc, _, err := l.Open(folder, file, ext)
		if err != nil {
			return nil, "", err
		}
		defer rc.Close()
		// zip needs random access; an EPUB fits in memory comfortably
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, "", err
		}
		data, mime, err := EPUBCover(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, "", ErrNoFile
		}
		return data, mime, nil
	}
	return nil, "", ErrNoFile
}

// Text converts a book (fb2 or txt) into HTML chapters for online reading.
func (l *Library) Text(folder, file, ext string, imgURL func(string) string) (*FB2Text, error) {
	rc, _, err := l.Open(folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	switch strings.ToLower(ext) {
	case "fb2":
		return ParseFB2Text(rc, imgURL)
	case "txt":
		return ParseTXT(rc)
	case "epub":
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		return EPUBText(bytes.NewReader(raw), int64(len(raw)), imgURL)
	}
	return nil, ErrNoFile
}

// Binary extracts an embedded file (illustration) from an FB2 by id.
func (l *Library) Binary(folder, file, ext, id string) ([]byte, string, error) {
	switch {
	case strings.EqualFold(ext, "fb2"):
		rc, _, err := l.Open(folder, file, ext)
		if err != nil {
			return nil, "", err
		}
		defer rc.Close()
		return ExtractBinary(rc, id)
	case strings.EqualFold(ext, "epub"):
		rc, _, err := l.Open(folder, file, ext)
		if err != nil {
			return nil, "", err
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, "", err
		}
		return EPUBBinary(bytes.NewReader(raw), int64(len(raw)), id)
	}
	return nil, "", ErrNoFile
}

// Meta extracts FB2 metadata (annotation, publication info) without the cover.
func (l *Library) Meta(folder, file, ext string) (*FB2Meta, error) {
	if !strings.EqualFold(ext, "fb2") {
		return nil, ErrNoFile
	}
	rc, _, err := l.Open(folder, file, ext)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return ParseFB2(rc, false)
}
