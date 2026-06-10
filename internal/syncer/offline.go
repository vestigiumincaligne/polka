package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var ErrOffline = errors.New("book is not available offline")

// MakeOffline downloads a book from the server into the local cache:
// the card from getBookForm, the file and the cover.
func (s *Syncer) MakeOffline(ctx context.Context, bookID int64) (*OfflineBook, error) {
	// Card
	resp, err := s.request(ctx, http.MethodGet,
		fmt.Sprintf("/main/getBooks/getBookForm?selectedItemID=%d", bookID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("book form: HTTP %d", resp.StatusCode)
	}
	var form struct {
		BookForm struct {
			Title        string  `json:"Title"`
			AuthorsNames string  `json:"AuthorsNames"`
			LibRate      float64 `json:"LibRate"`
			BookSize     int64   `json:"BookSize"`
			Ext          string  `json:"Ext"`
		} `json:"bookForm"`
		Series []struct {
			SeriesTitle string `json:"SeriesTitle"`
			SeqNumber   int    `json:"SeqNumber"`
		} `json:"series"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&form); err != nil {
		return nil, err
	}
	ext := strings.TrimPrefix(form.BookForm.Ext, ".")
	if ext == "" {
		ext = "fb2"
	}

	// Book file
	fresp, err := s.request(ctx, http.MethodGet, fmt.Sprintf("/Images/fb2/%d", bookID), nil)
	if err != nil {
		return nil, err
	}
	defer fresp.Body.Close()
	if fresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("book file: HTTP %d", fresp.StatusCode)
	}
	path := s.bookPath(bookID, ext)
	dst, err := os.Create(path + ".part")
	if err != nil {
		return nil, err
	}
	size, err := io.Copy(dst, fresp.Body)
	dst.Close()
	if err != nil {
		os.Remove(path + ".part")
		return nil, err
	}
	if err := os.Rename(path+".part", path); err != nil {
		return nil, err
	}

	// Cover (optional)
	var cover []byte
	var coverMime string
	if cresp, err := s.request(ctx, http.MethodGet, fmt.Sprintf("/Images/covers/%d", bookID), nil); err == nil {
		if cresp.StatusCode == http.StatusOK {
			cover, _ = io.ReadAll(io.LimitReader(cresp.Body, 4<<20))
			coverMime = cresp.Header.Get("Content-Type")
		}
		cresp.Body.Close()
	}

	book := &OfflineBook{
		ID:      bookID,
		Title:   form.BookForm.Title,
		Authors: form.BookForm.AuthorsNames,
		LibRate: form.BookForm.LibRate,
		Ext:     ext,
		Size:    size,
	}
	if len(form.Series) > 0 {
		book.SeriesTitle = form.Series[0].SeriesTitle
		book.SeqNumber = form.Series[0].SeqNumber
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO offline_books (id, title, authors, series_title, seq_number, year, lib_rate, ext, size, cover, cover_mime)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET title = excluded.title, authors = excluded.authors,
			series_title = excluded.series_title, seq_number = excluded.seq_number,
			lib_rate = excluded.lib_rate, ext = excluded.ext, size = excluded.size,
			cover = excluded.cover, cover_mime = excluded.cover_mime`,
		bookID, book.Title, book.Authors, book.SeriesTitle, book.SeqNumber,
		book.LibRate, ext, size, cover, coverMime)
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	s.log.Info("book saved offline", "id", bookID, "title", book.Title, "size", size)
	return book, nil
}

func (s *Syncer) RemoveOffline(ctx context.Context, bookID int64) error {
	b, err := s.OfflineBook(ctx, bookID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM offline_books WHERE id = ?`, bookID); err != nil {
		return err
	}
	os.Remove(s.bookPath(bookID, b.Ext))
	return nil
}

func (s *Syncer) bookPath(bookID int64, ext string) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.%s", bookID, ext))
}

func (s *Syncer) OfflineBook(ctx context.Context, bookID int64) (*OfflineBook, error) {
	var b OfflineBook
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, authors, series_title, seq_number, year, lib_rate, ext, size
		FROM offline_books WHERE id = ?`, bookID).
		Scan(&b.ID, &b.Title, &b.Authors, &b.SeriesTitle, &b.SeqNumber, &b.Year, &b.LibRate, &b.Ext, &b.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOffline
	}
	return &b, err
}

func (s *Syncer) OfflineBooks(ctx context.Context) ([]OfflineBook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, authors, series_title, seq_number, year, lib_rate, ext, size
		FROM offline_books ORDER BY saved_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OfflineBook
	for rows.Next() {
		var b OfflineBook
		if err := rows.Scan(&b.ID, &b.Title, &b.Authors, &b.SeriesTitle, &b.SeqNumber, &b.Year, &b.LibRate, &b.Ext, &b.Size); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// OfflineCover returns the cover from the cache.
func (s *Syncer) OfflineCover(ctx context.Context, bookID int64) ([]byte, string, error) {
	var data []byte
	var mime string
	err := s.db.QueryRowContext(ctx,
		`SELECT cover, cover_mime FROM offline_books WHERE id = ?`, bookID).Scan(&data, &mime)
	if errors.Is(err, sql.ErrNoRows) || len(data) == 0 {
		return nil, "", ErrOffline
	}
	return data, mime, err
}

// OpenOffline opens the local book file.
func (s *Syncer) OpenOffline(ctx context.Context, bookID int64) (io.ReadCloser, *OfflineBook, error) {
	b, err := s.OfflineBook(ctx, bookID)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(s.bookPath(bookID, b.Ext))
	if err != nil {
		return nil, nil, ErrOffline
	}
	return f, b, nil
}
