package library

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// EPUB online reading: spine documents are converted to sanitized HTML
// chapters, so the regular continuous reader (same as fb2/txt) renders
// them — no iframe-based engine on the client.

// epubAllowedTags is the whitelist for book content. Everything else is
// unwrapped (children kept, tag dropped).
var epubAllowedTags = map[string]bool{
	"p": true, "div": true, "span": true, "section": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"em": true, "i": true, "strong": true, "b": true, "small": true, "sub": true, "sup": true,
	"blockquote": true, "pre": true, "code": true, "cite": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "td": true, "th": true,
	"figure": true, "figcaption": true,
	"br": true, "hr": true, "img": true,
}

// EPUBText converts an EPUB into chapters for the online reader.
// imgURL maps an image path inside the archive to a public URL.
func EPUBText(r io.ReaderAt, size int64, imgURL func(string) string) (*FB2Text, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open epub: %w", err)
	}
	opfPath, err := epubRootFile(zr)
	if err != nil {
		return nil, err
	}

	var opf struct {
		Manifest struct {
			Items []struct {
				ID        string `xml:"id,attr"`
				Href      string `xml:"href,attr"`
				MediaType string `xml:"media-type,attr"`
			} `xml:"item"`
		} `xml:"manifest"`
		Spine struct {
			Refs []struct {
				IDRef  string `xml:"idref,attr"`
				Linear string `xml:"linear,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	rc, err := openZipFile(zr, opfPath)
	if err != nil {
		return nil, err
	}
	err = xml.NewDecoder(rc).Decode(&opf)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("parse opf: %w", err)
	}

	hrefByID := map[string]string{}
	for _, it := range opf.Manifest.Items {
		if strings.Contains(it.MediaType, "html") {
			hrefByID[it.ID] = it.Href
		}
	}

	opfDir := path.Dir(opfPath)
	res := &FB2Text{}
	for _, ref := range opf.Spine.Refs {
		if ref.Linear == "no" {
			continue
		}
		href, ok := hrefByID[ref.IDRef]
		if !ok {
			continue
		}
		docPath := path.Join(opfDir, href)
		rc, err := openZipFile(zr, docPath)
		if err != nil {
			continue
		}
		title, body := epubChapterHTML(rc, path.Dir(docPath), imgURL)
		rc.Close()
		if strings.TrimSpace(body) == "" {
			continue
		}
		res.Chapters = append(res.Chapters, FB2Chapter{Title: title, HTML: body})
	}
	if len(res.Chapters) == 0 {
		return nil, fmt.Errorf("epub: no readable chapters")
	}
	return res, nil
}

// epubChapterHTML extracts the body of a spine document as sanitized
// HTML. The chapter title is taken from the first heading.
func epubChapterHTML(r io.Reader, baseDir string, imgURL func(string) string) (title, body string) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", ""
	}
	var sb strings.Builder

	var findBody func(*html.Node) *html.Node
	findBody = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if b := findBody(c); b != nil {
				return b
			}
		}
		return nil
	}
	bodyNode := findBody(doc)
	if bodyNode == nil {
		return "", ""
	}

	var render func(*html.Node)
	render = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			sb.WriteString(html.EscapeString(n.Data))
			return
		case html.ElementNode:
			tag := strings.ToLower(n.Data)
			switch {
			case tag == "script" || tag == "style" || tag == "head" || tag == "iframe":
				return // drop entirely
			case tag == "image" || tag == "svg":
				// SVG-wrapped covers: keep nested <image> as <img>
				if tag == "image" {
					if src := nodeAttr(n, "href", "xlink:href"); src != "" {
						writeImg(&sb, baseDir, src, imgURL)
					}
					return
				}
			case tag == "img":
				if src := nodeAttr(n, "src"); src != "" {
					writeImg(&sb, baseDir, src, imgURL)
				}
				return
			case tag == "a":
				// External links are stripped to text; internal anchors too.
			}
			allowed := epubAllowedTags[tag]
			if title == "" && len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '4' {
				title = strings.TrimSpace(textContent(n))
			}
			if allowed {
				sb.WriteString("<" + tag + ">")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				render(c)
			}
			if allowed && !voidTag(tag) {
				sb.WriteString("</" + tag + ">")
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			render(c)
		}
	}
	render(bodyNode)
	return title, sb.String()
}

func writeImg(sb *strings.Builder, baseDir, src string, imgURL func(string) string) {
	if strings.Contains(src, "://") {
		return // remote images are dropped
	}
	full := path.Clean(path.Join(baseDir, src))
	sb.WriteString(`<img src="` + html.EscapeString(imgURL(url.PathEscape(full))) + `" loading="lazy"/>`)
}

func nodeAttr(n *html.Node, names ...string) string {
	for _, a := range n.Attr {
		for _, want := range names {
			if strings.EqualFold(a.Key, want) {
				return a.Val
			}
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func voidTag(tag string) bool { return tag == "br" || tag == "hr" || tag == "img" }

// EPUBBinary returns a file from inside the archive (chapter images).
// name is the PathEscape-d archive path produced by EPUBText.
func EPUBBinary(r io.ReaderAt, size int64, name string) ([]byte, string, error) {
	decoded, err := url.PathUnescape(name)
	if err != nil {
		return nil, "", err
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, "", err
	}
	rc, err := openZipFile(zr, decoded)
	if err != nil {
		return nil, "", ErrNoFile
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", err
	}
	mime := ""
	switch strings.ToLower(path.Ext(decoded)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".png":
		mime = "image/png"
	case ".gif":
		mime = "image/gif"
	case ".svg":
		mime = "image/svg+xml"
	case ".webp":
		mime = "image/webp"
	}
	return data, mime, nil
}
