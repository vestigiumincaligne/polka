// Package inpx reads inpx catalogs (MyHomeLib/FLibrary format):
// a zip archive with *.inp files where every line is a book record
// with fields separated by \x04. The field order is defined by structure.info,
// the collection name lives in collection.info.
package inpx

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

const (
	fieldsSeparator = '\x04'
	listSeparator   = ":"
	namesSeparator  = ","

	structureEntry  = "structure.info"
	collectionEntry = "collection.info"

	// Default field order when structure.info is missing.
	defaultStructure = "AUTHOR;GENRE;TITLE;SERIES;SERNO;FILE;SIZE;LIBID;DEL;EXT;DATE;LANG;LIBRATE;KEYWORDS;YEAR;SOURCELIB"
)

type Author struct {
	Last, First, Middle string
}

// Record is a single catalog entry (one book).
type Record struct {
	Authors   []Author
	Genres    []string
	Keywords  []string
	Title     string
	Series    string
	SeriesNum int
	File      string // file name without extension
	Ext       string
	Size      int64
	LibID     string
	Deleted   bool
	Date      string // YYYY-MM-DD — date added to the catalog
	Lang      string
	Rate      float64
	Year      int
	Folder    string // archive containing the book, e.g. fb2-000024-030559.zip
}

type File struct {
	zr     *zip.ReadCloser
	fields []string
	name   string
	desc   string
	inps   []*zip.File
}

func Open(filePath string) (*File, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("open inpx: %w", err)
	}

	f := &File{zr: zr}
	structure := defaultStructure
	for _, entry := range zr.File {
		switch strings.ToLower(entry.Name) {
		case structureEntry:
			if s, err := readAll(entry); err == nil && strings.TrimSpace(s) != "" {
				structure = s
			}
		case collectionEntry:
			if s, err := readAll(entry); err == nil {
				s = strings.TrimPrefix(s, "\uFEFF")
				lines := strings.SplitN(strings.ReplaceAll(s, "\r\n", "\n"), "\n", 2)
				f.name = strings.TrimSpace(lines[0])
				if len(lines) > 1 {
					f.desc = strings.TrimSpace(lines[1])
				}
			}
		default:
			if strings.EqualFold(path.Ext(entry.Name), ".inp") {
				f.inps = append(f.inps, entry)
			}
		}
	}
	for _, field := range strings.Split(structure, ";") {
		if field = strings.TrimSpace(field); field != "" {
			f.fields = append(f.fields, field)
		}
	}
	if len(f.inps) == 0 {
		zr.Close()
		return nil, fmt.Errorf("inpx contains no .inp entries")
	}
	return f, nil
}

func (f *File) Close() error { return f.zr.Close() }

// CollectionName returns the collection name from collection.info (may be empty).
func (f *File) CollectionName() string { return f.name }

func (f *File) CollectionDescription() string { return f.desc }

// Records sequentially calls fn for every record of all .inp files.
func (f *File) Records(fn func(*Record) error) error {
	for _, inp := range f.inps {
		if err := f.readInp(inp, fn); err != nil {
			return fmt.Errorf("%s: %w", inp.Name, err)
		}
	}
	return nil
}

func (f *File) readInp(inp *zip.File, fn func(*Record) error) error {
	rc, err := inp.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// The default book archive is the .inp name with a .zip extension;
	// the FOLDER field of a record takes priority.
	base := strings.TrimSuffix(path.Base(inp.Name), path.Ext(inp.Name))
	defaultFolder := base + ".zip"

	r := bufio.NewReaderSize(rc, 1<<20)
	for lineNo := 1; ; lineNo++ {
		line, err := r.ReadString('\n')
		if line != "" {
			if rec := parseLine(line, f.fields, defaultFolder); rec != nil {
				if err := fn(rec); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
}

func parseLine(line string, fields []string, defaultFolder string) *Record {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil
	}

	values := strings.Split(line, string(fieldsSeparator))
	rec := &Record{Folder: defaultFolder}
	for i, name := range fields {
		if i >= len(values) {
			break
		}
		v := strings.TrimSpace(values[i])
		switch name {
		case "AUTHOR":
			rec.Authors = parseAuthors(v)
		case "GENRE":
			rec.Genres = splitList(v)
		case "TITLE":
			rec.Title = v
		case "SERIES":
			rec.Series = v
		case "SERNO":
			rec.SeriesNum, _ = strconv.Atoi(v)
		case "FILE":
			rec.File = v
		case "SIZE":
			rec.Size, _ = strconv.ParseInt(v, 10, 64)
		case "LIBID":
			rec.LibID = v
		case "DEL":
			rec.Deleted = v == "1"
		case "EXT":
			rec.Ext = strings.TrimPrefix(v, ".")
		case "DATE":
			rec.Date = v
		case "LANG":
			rec.Lang = strings.ToLower(v)
		case "LIBRATE", "RATE":
			rec.Rate, _ = strconv.ParseFloat(v, 64)
		case "KEYWORDS":
			rec.Keywords = splitKeywords(v)
		case "YEAR":
			rec.Year, _ = strconv.Atoi(v)
		case "FOLDER":
			if v != "" {
				rec.Folder = v
			}
		}
	}

	if rec.Title == "" || rec.File == "" {
		return nil
	}
	return rec
}

func parseAuthors(s string) []Author {
	var authors []Author
	for _, one := range splitList(s) {
		parts := strings.SplitN(one, namesSeparator, 3)
		var a Author
		a.Last = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			a.First = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			a.Middle = strings.TrimSpace(parts[2])
		}
		if a.Last != "" || a.First != "" || a.Middle != "" {
			authors = append(authors, a)
		}
	}
	return authors
}

func splitList(s string) []string {
	var items []string
	for _, item := range strings.Split(s, listSeparator) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// splitKeywords splits on ':', and when absent — on ',':
// both variants occur in real-world inpx files.
func splitKeywords(s string) []string {
	if strings.Contains(s, listSeparator) || !strings.Contains(s, namesSeparator) {
		return splitList(s)
	}
	var items []string
	for _, item := range strings.Split(s, namesSeparator) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func readAll(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	return string(b), err
}
