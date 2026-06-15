// Package library — доступ к файлам книг: архивы (zip) и папки
// внутри корня библиотеки, извлечение обложек и метаданных FB2.
package library

import (
	"archive/zip"

	"bytes"
	"errors"
	"fmt"
	"github.com/bodgit/sevenzip"
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
	covers map[int64]coverEntry // ограниченный кэш обложек
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

// Open возвращает содержимое файла книги: folder — архив или подпапка
// в корне библиотеки, имя файла — file + "." + ext.
func (l *Library) Open(folder, file, ext string) (io.ReadCloser, int64, error) {
	name := file + "." + ext
	// Не выпускаем за корень: folder/file берутся из метаданных каталога
	// (в т.ч. импортированного inpx), поэтому «../» не должен дать прочитать
	// файлы вне коллекции.
	if !pathInside(l.root, folder) || strings.ContainsAny(name, `/\`) {
		return nil, 0, fmt.Errorf("%w: %s/%s", ErrNoFile, folder, name)
	}
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

	// Архив с книгами. inpx часто указывает имя архива как .zip, а на
	// диске лежит .7z (дампы Флибусты), поэтому пробуем оба расширения.
	for _, ap := range archiveCandidates(path) {
		if rc, size, err := openFromArchive(ap, name); err == nil {
			return rc, size, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: %s in %s", ErrNoFile, name, folder)
}

// archiveCandidates возвращает возможные пути к архиву: сам путь и
// вариант с заменой .zip↔.7z (на случай рассинхрона inpx и диска).
func archiveCandidates(path string) []string {
	out := []string{path}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		out = append(out, strings.TrimSuffix(path, filepath.Ext(path))+".7z")
	case ".7z":
		out = append(out, strings.TrimSuffix(path, filepath.Ext(path))+".zip")
	}
	return out
}

// openFromArchive достаёт файл name из архива path (zip или 7z).
func openFromArchive(path, name string) (io.ReadCloser, int64, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, 0, err
	}
	if strings.EqualFold(filepath.Ext(path), ".7z") {
		return open7z(path, name)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, 0, err
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
	return nil, 0, ErrNoFile
}

func open7z(path, name string) (io.ReadCloser, int64, error) {
	zr, err := sevenzip.OpenReader(path)
	if err != nil {
		return nil, 0, err
	}
	for _, entry := range zr.File {
		if strings.EqualFold(entry.Name, name) {
			rc, err := entry.Open()
			if err != nil {
				zr.Close()
				return nil, 0, err
			}
			return &sevenzEntryReader{rc: rc, zr: zr}, int64(entry.UncompressedSize), nil
		}
	}
	zr.Close()
	return nil, 0, ErrNoFile
}

type sevenzEntryReader struct {
	rc io.ReadCloser
	zr *sevenzip.ReadCloser
}

func (z *sevenzEntryReader) Read(p []byte) (int, error) { return z.rc.Read(p) }

func (z *sevenzEntryReader) Close() error {
	z.rc.Close()
	return z.zr.Close()
}

// pathInside проверяет, что root/rel после очистки остаётся под root,
// отсекая «../» и абсолютные компоненты.
func pathInside(root, rel string) bool {
	full := filepath.Join(root, filepath.FromSlash(rel))
	r, err := filepath.Rel(root, full)
	if err != nil {
		return false
	}
	return r == "." || (!strings.HasPrefix(r, ".."+string(os.PathSeparator)) && r != "..")
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

// Cover возвращает обложку книги (для fb2), с кэшем в памяти.
func (l *Library) Cover(bookID int64, folder, file, ext string) ([]byte, string, error) {
	l.mu.Lock()
	if c, ok := l.covers[bookID]; ok {
		l.mu.Unlock()
		if c.data == nil {
			return nil, "", ErrNoFile // отрицательный кэш
		}
		return c.data, c.mime, nil
	}
	l.mu.Unlock()

	data, mime, err := l.extractCover(folder, file, ext)

	l.mu.Lock()
	if len(l.covers) >= coverCacheSize {
		// простое вытеснение: сбрасываем кэш целиком
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

// Text конвертирует книгу (fb2 или txt) в HTML-главы для онлайн-чтения.
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

// Binary достаёт вложенный файл (иллюстрацию) из FB2 по id.
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

// Meta извлекает метаданные FB2 (аннотация, выходные данные) без обложки.
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
