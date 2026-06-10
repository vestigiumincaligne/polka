package library

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html/charset"
)

// FB2Meta holds book metadata from an FB2 file.
type FB2Meta struct {
	Title          string
	Authors        []PersonName
	Series         string
	SeriesNum      int
	Genres         []string
	Lang           string
	AnnotationHTML string
	Publisher      string
	City           string
	Year           string
	ISBN           string
	Cover          []byte
	CoverMime      string
}

type PersonName struct {
	First, Middle, Last string
}

// ParseFB2 parses an FB2 as a stream. With withCover=true it reads on
// until the <binary> with the cover; otherwise it stops after <description>.
func ParseFB2(r io.Reader, withCover bool) (*FB2Meta, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false

	meta := &FB2Meta{}
	var coverID string
	var path []string

	in := func(elems ...string) bool {
		if len(path) < len(elems) {
			return false
		}
		tail := path[len(path)-len(elems):]
		for i := range elems {
			if tail[i] != elems[i] {
				return false
			}
		}
		return true
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("fb2 parse: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case name == "annotation" && in("description", "title-info"):
				htmlStr, err := annotationToHTML(dec, t)
				if err != nil {
					return nil, err
				}
				meta.AnnotationHTML = htmlStr
				continue // annotationToHTML has already consumed the EndElement

			case name == "image" && in("description", "title-info", "coverpage"):
				for _, a := range t.Attr {
					if a.Name.Local == "href" {
						coverID = strings.TrimPrefix(a.Value, "#")
					}
				}

			case name == "author" && in("description", "title-info"):
				meta.Authors = append(meta.Authors, PersonName{})

			case name == "sequence" && in("description", "title-info"):
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "name":
						if meta.Series == "" {
							meta.Series = a.Value
						}
					case "number":
						if meta.SeriesNum == 0 {
							meta.SeriesNum, _ = strconv.Atoi(a.Value)
						}
					}
				}

			case name == "binary":
				var id, ctype string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "id":
						id = a.Value
					case "content-type":
						ctype = a.Value
					}
				}
				// Without a declared coverpage, treat the book's first image
				// as the cover — a common case in real-world collections.
				take := coverID != "" && id == coverID ||
					coverID == "" && strings.HasPrefix(ctype, "image/")
				if withCover && take {
					var b64 string
					if err := dec.DecodeElement(&b64, &t); err != nil {
						return nil, err
					}
					data, err := base64.StdEncoding.DecodeString(strings.Map(dropSpace, b64))
					if err != nil {
						return nil, fmt.Errorf("cover base64: %w", err)
					}
					meta.Cover = data
					meta.CoverMime = ctype
					return meta, nil
				}
				if err := dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			path = append(path, name)

		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			if t.Name.Local == "description" && !withCover {
				return meta, nil
			}

		case xml.CharData:
			if len(path) < 2 {
				continue
			}
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}
			elem, parent := path[len(path)-1], path[len(path)-2]
			switch parent {
			case "publish-info":
				switch elem {
				case "publisher":
					meta.Publisher += text
				case "city":
					meta.City += text
				case "year":
					meta.Year += text
				case "isbn":
					meta.ISBN += text
				}
			case "title-info":
				switch elem {
				case "book-title":
					meta.Title += text
				case "lang":
					meta.Lang += strings.ToLower(text)
				case "genre":
					meta.Genres = append(meta.Genres, text)
				}
			case "author":
				if len(path) < 3 || path[len(path)-3] != "title-info" || len(meta.Authors) == 0 {
					continue
				}
				a := &meta.Authors[len(meta.Authors)-1]
				switch elem {
				case "first-name":
					a.First += text
				case "middle-name":
					a.Middle += text
				case "last-name":
					a.Last += text
				}
			}
		}
	}
	return meta, nil
}

func decodeBase64(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.Map(dropSpace, b64))
}

func dropSpace(r rune) rune {
	if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
		return -1
	}
	return r
}

// annotationTags maps annotation fb2 elements to their HTML counterparts.
var annotationTags = map[string]string{
	"p":             "p",
	"emphasis":      "em",
	"strong":        "strong",
	"strikethrough": "s",
	"sub":           "sub",
	"sup":           "sup",
	"code":          "code",
	"cite":          "blockquote",
	"poem":          "blockquote",
	"stanza":        "p",
	"subtitle":      "h4",
	"table":         "table",
	"tr":            "tr",
	"td":            "td",
	"th":            "th",
}

// annotationToHTML reads the <annotation> subtree and converts it to HTML.
func annotationToHTML(dec *xml.Decoder, _ xml.StartElement) (string, error) {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("annotation: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "empty-line":
				sb.WriteString("<br/>")
			case "v": // verse line
			default:
				if tag, ok := annotationTags[t.Name.Local]; ok {
					sb.WriteString("<" + tag + ">")
				}
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				break
			}
			switch t.Name.Local {
			case "empty-line":
			case "v":
				sb.WriteString("<br/>")
			default:
				if tag, ok := annotationTags[t.Name.Local]; ok {
					sb.WriteString("</" + tag + ">")
				}
			}
		case xml.CharData:
			sb.WriteString(html.EscapeString(string(t)))
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

var binaryRe = regexp.MustCompile(`(?s)<binary[^>]*>.*?</binary>`)

// StripBinaries removes all embedded images (<binary>) from an FB2.
func StripBinaries(data []byte) []byte {
	return binaryRe.ReplaceAll(data, nil)
}
