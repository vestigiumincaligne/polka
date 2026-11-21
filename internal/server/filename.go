package server

import (
	"fmt"
	"mime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vestigiumincaligne/polka/internal/store"
)

// Download file names follow "ID - Author - Series - NN - Title.ext".
// Missing parts are omitted, Unicode (Cyrillic included) is preserved and
// characters that common file systems reject are removed.

const (
	fileNameSep      = " - "
	maxFileNameBytes = 200 // base name without extension; well under the 255-byte limit
	maxSeqNumber     = 9999
	maxNamedAuthors  = 2
	etAlSuffix       = " и др."
)

type fileNameParts struct {
	ID        int64
	Authors   []string // display names, already in "First Last" order
	Series    string
	SeqNumber int
	Title     string
	Fallback  string // used when there is no title (e.g. the archive file name)
}

func partsFromBookFile(f *store.BookFile) fileNameParts {
	return fileNameParts{
		ID:        f.ID,
		Authors:   f.Authors,
		Series:    f.Series,
		SeqNumber: f.SeqNumber,
		Title:     f.Title,
		Fallback:  f.File,
	}
}

// bookBaseName builds the file name without extension.
func bookBaseName(p fileNameParts) string {
	var head []string
	if p.ID > 0 {
		head = append(head, strconv.FormatInt(p.ID, 10))
	}
	if a := formatAuthors(p.Authors); a != "" {
		head = append(head, a)
	}
	if s := cleanNamePart(p.Series); s != "" {
		head = append(head, s)
		if p.SeqNumber > 0 && p.SeqNumber <= maxSeqNumber {
			head = append(head, fmt.Sprintf("%02d", p.SeqNumber))
		}
	}
	title := cleanNamePart(p.Title)
	if title == "" {
		title = cleanNamePart(p.Fallback)
	}

	prefix := strings.Join(head, fileNameSep)
	if title == "" {
		return truncateUTF8(prefix, maxFileNameBytes)
	}
	if prefix == "" {
		return truncateUTF8(title, maxFileNameBytes)
	}
	// Shorten the title first so the identifying prefix survives.
	if room := maxFileNameBytes - len(prefix) - len(fileNameSep); room >= 16 {
		title = strings.TrimRightFunc(truncateUTF8(title, room), isEdgeJunk)
		return prefix + fileNameSep + title
	}
	return strings.TrimRightFunc(truncateUTF8(prefix+fileNameSep+title, maxFileNameBytes), isEdgeJunk)
}

// bookFileName returns the full download name with extension.
func bookFileName(p fileNameParts, ext string) string {
	base := bookBaseName(p)
	if base == "" {
		base = "book"
	}
	if isReservedWindowsName(base) {
		base += "_"
	}
	if ext = cleanNamePart(ext); ext != "" {
		return base + "." + ext
	}
	return base
}

func formatAuthors(authors []string) string {
	var names []string
	for _, a := range authors {
		if a = cleanNamePart(a); a != "" {
			names = append(names, a)
		}
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= maxNamedAuthors:
		return strings.Join(names, ", ")
	default:
		return names[0] + etAlSuffix
	}
}

// cleanNamePart drops characters rejected by Windows/macOS/Linux file systems
// and control characters, collapses whitespace and trims separator junk from
// the edges so joined parts never produce "a -  - b" or trailing dots.
func cleanNamePart(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return ' '
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimFunc(s, isEdgeJunk)
}

func isEdgeJunk(r rune) bool {
	return r == ' ' || r == '.' || r == '-' || r == '_' || r == ','
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func isReservedWindowsName(base string) bool {
	name := strings.ToUpper(base)
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '1' && name[3] <= '9' {
		return true
	}
	return false
}

// contentDisposition builds a header value with a plain ASCII filename for
// old clients and an RFC 5987 filename* carrying the full UTF-8 name.
func contentDisposition(kind, name, asciiFallback string) string {
	fallback := asciiFileName(asciiFallback)
	if fallback == "" {
		fallback = asciiFileName(name)
	}
	if fallback == "" {
		fallback = "book"
	}
	v := kind + `; filename="` + fallback + `"`
	if fallback != name {
		if ext := mime.FormatMediaType(kind, map[string]string{"filename": name}); ext != "" {
			// FormatMediaType emits filename*=utf-8''... for non-ASCII names.
			if i := strings.Index(ext, "filename*="); i >= 0 {
				v += "; " + ext[i:]
			} else if i := strings.Index(ext, "filename="); i >= 0 {
				v = kind + "; " + ext[i:]
			}
		}
	}
	return v
}

func asciiFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == ';' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// idFileName is the ASCII fallback name: "<book id>.<ext>".
func idFileName(id int64, ext string) string {
	return strconv.FormatInt(id, 10) + "." + ext
}
