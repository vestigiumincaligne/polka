package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// FB2Text is the book's full text split into chapters, plus footnotes.
type FB2Text struct {
	Chapters []FB2Chapter
	Notes    map[string]string // footnote id -> HTML
}

type FB2Chapter struct {
	Title string
	HTML  string
}

// ParseFB2Text converts the FB2 body to HTML. Chapters are the top-level
// sections of the first <body>; <body name="notes"> goes into the footnotes map.
// imgURL builds an image URL from a <binary> id.
func ParseFB2Text(r io.Reader, imgURL func(id string) string) (*FB2Text, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false

	c := &fb2Converter{imgURL: imgURL}
	res := &FB2Text{Notes: map[string]string{}}
	mainDone := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("fb2 text: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "body":
			name := attrValue(start, "name")
			switch {
			case name == "notes" || name == "comments":
				if err := c.parseNotesBody(dec, res); err != nil {
					return nil, err
				}
			case !mainDone:
				if err := c.parseMainBody(dec, res); err != nil {
					return nil, err
				}
				mainDone = true
			default:
				dec.Skip()
			}
		case "binary":
			// Binary data comes after the text — nothing left to read.
			return res, nil
		}
	}
	return res, nil
}

type fb2Converter struct {
	imgURL func(string) string
}

func attrValue(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// minSubChapters is the threshold at which nested sections ("chapters" inside
// a "part") are expanded into separate table-of-contents chapters.
const minSubChapters = 3

// parseMainBody collects the chapters. Content before the first section becomes the "preface".
func (c *fb2Converter) parseMainBody(dec *xml.Decoder, res *FB2Text) error {
	var preface strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "section":
				title, own, subs, err := c.convertSectionSplit(dec)
				if err != nil {
					return err
				}
				if len(subs) >= minSubChapters {
					// A "part" with chapters: its intro becomes a separate page,
					// each chapter becomes a table-of-contents entry.
					if title != "" || strings.TrimSpace(own) != "" {
						res.Chapters = append(res.Chapters, FB2Chapter{Title: title, HTML: own})
					}
					res.Chapters = append(res.Chapters, subs...)
					continue
				}
				// Too few nested sections — merge them back in with subheadings.
				var sb strings.Builder
				sb.WriteString(own)
				for _, sub := range subs {
					if sub.Title != "" {
						sb.WriteString("<h3>" + escape(sub.Title) + "</h3>")
					}
					sb.WriteString(sub.HTML)
				}
				if html := sb.String(); strings.TrimSpace(html) != "" || title != "" {
					res.Chapters = append(res.Chapters, FB2Chapter{Title: title, HTML: html})
				}
			case "title":
				// The whole-book title inside the body — skip it, it is already on the card.
				if err := dec.Skip(); err != nil {
					return err
				}
			default:
				if err := c.convertElement(dec, t, &preface); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "body" {
				if s := strings.TrimSpace(preface.String()); s != "" {
					res.Chapters = append([]FB2Chapter{{HTML: s}}, res.Chapters...)
				}
				return nil
			}
		}
	}
}

// convertSectionSplit parses a top-level section, returning its own
// content and its first-level nested sections separately.
func (c *fb2Converter) convertSectionSplit(dec *xml.Decoder) (string, string, []FB2Chapter, error) {
	var title string
	var own strings.Builder
	var subs []FB2Chapter
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", "", nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "title":
				text, err := c.collectTitle(dec)
				if err != nil {
					return "", "", nil, err
				}
				if title == "" {
					title = text
				} else {
					own.WriteString("<h3>" + escape(text) + "</h3>")
				}
			case "section":
				subTitle, subHTML, err := c.convertSection(dec)
				if err != nil {
					return "", "", nil, err
				}
				subs = append(subs, FB2Chapter{Title: subTitle, HTML: subHTML})
			default:
				if err := c.convertElement(dec, t, &own); err != nil {
					return "", "", nil, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "section" {
				return title, own.String(), subs, nil
			}
		}
	}
}

func (c *fb2Converter) parseNotesBody(dec *xml.Decoder, res *FB2Text) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "section" {
				id := attrValue(t, "id")
				title, html, err := c.convertSection(dec)
				if err != nil {
					return err
				}
				if id != "" {
					if title != "" {
						html = "<strong>" + escape(title) + "</strong> " + html
					}
					res.Notes[id] = html
				}
			}
		case xml.EndElement:
			if t.Name.Local == "body" {
				return nil
			}
		}
	}
}

// convertSection reads a section to its end; returns the title (as text)
// and the content HTML. Nested sections are merged in with subheadings.
func (c *fb2Converter) convertSection(dec *xml.Decoder) (string, string, error) {
	var title string
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "title":
				text, err := c.collectTitle(dec)
				if err != nil {
					return "", "", err
				}
				if title == "" {
					title = text
				} else {
					sb.WriteString("<h3>" + escape(text) + "</h3>")
				}
			case "section":
				subTitle, subHTML, err := c.convertSection(dec)
				if err != nil {
					return "", "", err
				}
				if subTitle != "" {
					sb.WriteString("<h3>" + escape(subTitle) + "</h3>")
				}
				sb.WriteString(subHTML)
			default:
				if err := c.convertElement(dec, t, &sb); err != nil {
					return "", "", err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "section" {
				return title, sb.String(), nil
			}
		}
	}
}

// collectTitle gathers the title text (lines joined with a separator).
func (c *fb2Converter) collectTitle(dec *xml.Decoder) (string, error) {
	var parts []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if s := strings.TrimSpace(string(t)); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, ". "), nil
}

// textTags is a simple fb2 element -> HTML tag mapping.
var textTags = map[string]string{
	"p":             "p",
	"emphasis":      "em",
	"strong":        "strong",
	"strikethrough": "s",
	"sub":           "sub",
	"sup":           "sup",
	"code":          "code",
	"subtitle":      "h4",
	"table":         "table",
	"tr":            "tr",
	"td":            "td",
	"th":            "th",
}

// classTags lists elements that become blocks with a css class.
var classTags = map[string][2]string{
	"epigraph":    {"div", "fb2-epigraph"},
	"cite":        {"blockquote", "fb2-cite"},
	"poem":        {"div", "fb2-poem"},
	"stanza":      {"div", "fb2-stanza"},
	"text-author": {"p", "fb2-text-author"},
	"annotation":  {"div", "fb2-annotation"},
}

// convertElement writes the HTML of element start (and its whole subtree) to sb.
func (c *fb2Converter) convertElement(dec *xml.Decoder, start xml.StartElement, sb *strings.Builder) error {
	name := start.Name.Local
	switch name {
	case "empty-line":
		sb.WriteString("<br/>")
		return dec.Skip()

	case "image":
		id := strings.TrimPrefix(attrValue(start, "href"), "#")
		if id != "" && c.imgURL != nil {
			fmt.Fprintf(sb, `<img class="fb2-img" loading="lazy" src="%s" alt=""/>`, html.EscapeString(c.imgURL(id)))
		}
		return dec.Skip()

	case "a":
		href := attrValue(start, "href")
		noteID := strings.TrimPrefix(href, "#")
		isNote := attrValue(start, "type") == "note" || (strings.HasPrefix(href, "#") && noteID != "")
		text, err := c.collectTitle(dec) // flat link text
		if err != nil {
			return err
		}
		switch {
		case isNote:
			fmt.Fprintf(sb, `<sup class="fb2-note-ref"><a data-note="%s">%s</a></sup>`,
				html.EscapeString(noteID), escape(text))
		case strings.HasPrefix(href, "http"):
			fmt.Fprintf(sb, `<a href="%s" target="_blank" rel="noopener">%s</a>`,
				html.EscapeString(href), escape(text))
		default:
			sb.WriteString(escape(text))
		}
		return nil

	case "v": // verse line
		sb.WriteString(`<p class="fb2-verse">`)
		if err := c.convertChildren(dec, sb); err != nil {
			return err
		}
		sb.WriteString("</p>")
		return nil

	case "title": // title inside poem/epigraph
		text, err := c.collectTitle(dec)
		if err != nil {
			return err
		}
		sb.WriteString("<h4>" + escape(text) + "</h4>")
		return nil
	}

	if pair, ok := classTags[name]; ok {
		fmt.Fprintf(sb, `<%s class="%s">`, pair[0], pair[1])
		if err := c.convertChildren(dec, sb); err != nil {
			return err
		}
		fmt.Fprintf(sb, "</%s>", pair[0])
		return nil
	}
	if tag, ok := textTags[name]; ok {
		sb.WriteString("<" + tag + ">")
		if err := c.convertChildren(dec, sb); err != nil {
			return err
		}
		sb.WriteString("</" + tag + ">")
		return nil
	}
	// Unknown element: unwrap its content with no wrapper.
	return c.convertChildren(dec, sb)
}

// convertChildren processes the current element's subtree until its end.
func (c *fb2Converter) convertChildren(dec *xml.Decoder, sb *strings.Builder) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := c.convertElement(dec, t, sb); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		case xml.CharData:
			sb.WriteString(escape(string(t)))
		}
	}
}

func escape(s string) string { return html.EscapeString(s) }

// ContentHash computes the SHA-256 of the book's normalized text (only
// lowercase letters and digits from <body>) — a content fingerprint
// resilient to changes in metadata, formatting, and encoding.
func ContentHash(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false

	h := sha256.New()
	inBody := 0
	buf := make([]byte, 0, 256)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("content hash: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "body" {
				inBody++
			} else if t.Name.Local == "binary" {
				dec.Skip()
			}
		case xml.EndElement:
			if t.Name.Local == "body" {
				inBody--
			}
		case xml.CharData:
			if inBody <= 0 {
				continue
			}
			buf = buf[:0]
			for _, r := range strings.ToLower(string(t)) {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					buf = utf8.AppendRune(buf, r)
				}
			}
			h.Write(buf)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractBinary extracts an embedded file (image) from an FB2 by id.
func ExtractBinary(r io.Reader, id string) ([]byte, string, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, "", ErrNoFile
		}
		if err != nil {
			return nil, "", err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "binary" || attrValue(start, "id") != id {
			continue
		}
		ctype := attrValue(start, "content-type")
		var b64 string
		if err := dec.DecodeElement(&b64, &start); err != nil {
			return nil, "", err
		}
		data, err := decodeBase64(b64)
		if err != nil {
			return nil, "", err
		}
		return data, ctype, nil
	}
}
