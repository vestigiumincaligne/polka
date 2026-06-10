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
