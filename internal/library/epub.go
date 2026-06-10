package library

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// EPUBMeta holds metadata from the EPUB OPF manifest.
type EPUBMeta struct {
	Title    string
	Authors  []string // full names as written in dc:creator
	Language string
	Subjects []string
}

// ParseEPUBFile reads the metadata of an EPUB file on disk.
func ParseEPUBFile(filePath string) (*EPUBMeta, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("open epub: %w", err)
	}
	defer zr.Close()

	opfPath, err := epubRootFile(&zr.Reader)
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, opfPath) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return parseOPF(rc)
		}
	}
	return nil, fmt.Errorf("epub: %s not found", opfPath)
}

// epubRootFile extracts the OPF path from META-INF/container.xml.
func epubRootFile(zr *zip.Reader) (string, error) {
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, "META-INF/container.xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()

		var container struct {
			Rootfiles []struct {
				FullPath string `xml:"full-path,attr"`
			} `xml:"rootfiles>rootfile"`
		}
		if err := xml.NewDecoder(rc).Decode(&container); err != nil {
			return "", fmt.Errorf("container.xml: %w", err)
		}
		if len(container.Rootfiles) == 0 {
			break
		}
		return path.Clean(container.Rootfiles[0].FullPath), nil
	}
	return "", fmt.Errorf("epub: container.xml has no rootfile")
}

func parseOPF(r io.Reader) (*EPUBMeta, error) {
	var opf struct {
		Metadata struct {
			Titles    []string `xml:"title"`
			Creators  []string `xml:"creator"`
			Languages []string `xml:"language"`
			Subjects  []string `xml:"subject"`
		} `xml:"metadata"`
	}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	if err := dec.Decode(&opf); err != nil {
		return nil, fmt.Errorf("opf: %w", err)
	}

	meta := &EPUBMeta{}
	if len(opf.Metadata.Titles) > 0 {
		meta.Title = strings.TrimSpace(opf.Metadata.Titles[0])
	}
	for _, c := range opf.Metadata.Creators {
		if c = strings.TrimSpace(c); c != "" {
			meta.Authors = append(meta.Authors, c)
		}
	}
	if len(opf.Metadata.Languages) > 0 {
		meta.Language = strings.ToLower(strings.TrimSpace(opf.Metadata.Languages[0]))
	}
	for _, s := range opf.Metadata.Subjects {
		if s = strings.TrimSpace(s); s != "" {
			meta.Subjects = append(meta.Subjects, s)
		}
	}
	return meta, nil
}

// EPUBCover extracts the cover image: an EPUB3 manifest item with
// properties="cover-image", or the EPUB2 <meta name="cover"> pointer.
func EPUBCover(r io.ReaderAt, size int64) ([]byte, string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, "", fmt.Errorf("open epub: %w", err)
	}
	opfPath, err := epubRootFile(zr)
	if err != nil {
		return nil, "", err
	}

	var opf struct {
		Metadata struct {
			Metas []struct {
				Name    string `xml:"name,attr"`
				Content string `xml:"content,attr"`
			} `xml:"meta"`
		} `xml:"metadata"`
		Manifest struct {
			Items []struct {
				ID         string `xml:"id,attr"`
				Href       string `xml:"href,attr"`
				MediaType  string `xml:"media-type,attr"`
				Properties string `xml:"properties,attr"`
			} `xml:"item"`
		} `xml:"manifest"`
	}
	rc, err := openZipFile(zr, opfPath)
	if err != nil {
		return nil, "", err
	}
	err = xml.NewDecoder(rc).Decode(&opf)
	rc.Close()
	if err != nil {
		return nil, "", fmt.Errorf("parse opf: %w", err)
	}

	href, mime := "", ""
	for _, it := range opf.Manifest.Items {
		if strings.Contains(it.Properties, "cover-image") {
			href, mime = it.Href, it.MediaType
			break
		}
	}
	if href == "" {
		coverID := ""
		for _, m := range opf.Metadata.Metas {
			if strings.EqualFold(m.Name, "cover") {
				coverID = m.Content
				break
			}
		}
		for _, it := range opf.Manifest.Items {
			if coverID != "" && it.ID == coverID {
				href, mime = it.Href, it.MediaType
				break
			}
		}
	}
	if href == "" {
		return nil, "", fmt.Errorf("epub: no cover")
	}

	coverPath := path.Join(path.Dir(opfPath), href)
	rc, err = openZipFile(zr, coverPath)
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", err
	}
	return data, mime, nil
}

func openZipFile(zr *zip.Reader, name string) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, name) || strings.EqualFold(f.Name, strings.TrimPrefix(name, "./")) {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("epub: %s not found", name)
}

// subjectGenres maps common English subject keywords (Gutenberg, LCSH,
// publisher metadata) to FB2 genre codes. Substring match, lowercase.
var subjectGenres = []struct{ key, code string }{
	{"science fiction", "sf"},
	{"fantasy", "sf_fantasy"},
	{"fairy tales", "child_tale"},
	{"horror", "sf_horror"},
	{"ghost", "sf_horror"},
	{"gothic", "sf_horror"},
	{"detective", "detective"},
	{"mystery", "detective"},
	{"crime", "det_crime"},
	{"love stories", "love"},
	{"romance", "love"},
	{"sea stories", "adv_maritime"},
	{"pirates", "adv_maritime"},
	{"western", "adv_western"},
	{"adventure", "adventure"},
	{"treasure", "adventure"},
	{"historical fiction", "prose_history"},
	{"war stories", "prose_military"},
	{"satire", "humor_prose"},
	{"humor", "humor_prose"},
	{"comic", "humor_prose"},
	{"juvenile", "children"},
	{"children", "children"},
	{"poetry", "poetry"},
	{"drama", "dramaturgy"},
	{"biography", "nonf_biography"},
	{"autobiograph", "nonf_biography"},
	{"philosophy", "sci_philosophy"},
	{"psychology", "sci_psychology"},
	{"mythology", "antique_myths"},
	{"epic literature", "antique_myths"},
	{"classical literature", "antique_ant"},
	{"bildungsromans", "prose_classic"},
	{"domestic fiction", "prose_classic"},
	{"psychological fiction", "prose_classic"},
	{"political fiction", "prose_classic"},
	{"didactic fiction", "prose_classic"},
	{"young women", "prose_classic"},
	{"england", "prose_classic"},
	{"fiction", "prose_classic"}, // generic fallback, keep last
}

// GenresFromSubjects derives FB2 genre codes from free-form subject
// strings (dc:subject). Up to three distinct genres, ordered by the
// first matching subject.
func GenresFromSubjects(subjects []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, pair := range subjectGenres {
		for _, subj := range subjects {
			if strings.Contains(strings.ToLower(subj), pair.key) && !seen[pair.code] {
				seen[pair.code] = true
				out = append(out, pair.code)
				break
			}
		}
		if len(out) >= 3 {
			break
		}
	}
	return out
}
