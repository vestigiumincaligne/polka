package inpx

import (
	"strings"
	"testing"
)

func defaultFields() []string {
	return strings.Split(defaultStructure, ";")
}

func TestParseLineDefaultStructure(t *testing.T) {
	sep := string(rune(fieldsSeparator))
	line := strings.Join([]string{
		"Толстой,Лев,Николаевич:Иванов,Иван,", // AUTHOR
		"prose_classic:prose_rus_classic",     // GENRE
		"Война и мир",                         // TITLE
		"Собрание сочинений",                  // SERIES
		"4",                                   // SERNO
		"12345",                               // FILE
		"1048576",                             // SIZE
		"98765",                               // LIBID
		"0",                                   // DEL
		"fb2",                                 // EXT
		"2024-05-01",                          // DATE
		"RU",                                  // LANG
		"5",                                   // LIBRATE
		"война:классика",                      // KEYWORDS
		"1869",                                // YEAR
		"lib.rus.ec",                          // SOURCELIB
	}, sep) + sep + "\r\n"

	rec := parseLine(line, defaultFields(), "fb2-000001.zip")
	if rec == nil {
		t.Fatal("record not parsed")
	}
	if len(rec.Authors) != 2 {
		t.Fatalf("authors = %v, want 2", rec.Authors)
	}
	if rec.Authors[0] != (Author{Last: "Толстой", First: "Лев", Middle: "Николаевич"}) {
		t.Errorf("author[0] = %+v", rec.Authors[0])
	}
	if len(rec.Genres) != 2 || rec.Genres[0] != "prose_classic" {
		t.Errorf("genres = %v", rec.Genres)
	}
	if rec.Title != "Война и мир" || rec.Series != "Собрание сочинений" || rec.SeriesNum != 4 {
		t.Errorf("title/series: %+v", rec)
	}
	if rec.File != "12345" || rec.Ext != "fb2" || rec.Size != 1048576 {
		t.Errorf("file fields: %+v", rec)
	}
	if rec.Deleted || rec.Lang != "ru" || rec.Rate != 5 || rec.Year != 1869 {
		t.Errorf("misc fields: %+v", rec)
	}
	if len(rec.Keywords) != 2 || rec.Keywords[1] != "классика" {
		t.Errorf("keywords = %v", rec.Keywords)
	}
	if rec.Folder != "fb2-000001.zip" {
		t.Errorf("folder = %q, want default", rec.Folder)
	}
}

func TestParseLineCustomStructureWithFolder(t *testing.T) {
	sep := string(rune(fieldsSeparator))
	fields := []string{"TITLE", "FILE", "EXT", "FOLDER", "DEL"}
	line := strings.Join([]string{"Книга", "file1", "epub", "books/custom.zip", "1"}, sep)

	rec := parseLine(line, fields, "default.zip")
	if rec == nil {
		t.Fatal("record not parsed")
	}
	if rec.Folder != "books/custom.zip" {
		t.Errorf("folder = %q", rec.Folder)
	}
	if !rec.Deleted {
		t.Error("deleted flag lost")
	}
}

func TestParseLineSkipsBroken(t *testing.T) {
	if rec := parseLine("\r\n", defaultFields(), "x.zip"); rec != nil {
		t.Error("empty line must be skipped")
	}
	sep := string(rune(fieldsSeparator))
	noTitle := strings.Join([]string{"Автор,,", "genre", "", "", "", "file", ""}, sep)
	if rec := parseLine(noTitle, defaultFields(), "x.zip"); rec != nil {
		t.Error("record without title must be skipped")
	}
}

func TestSplitKeywordsCommaFallback(t *testing.T) {
	got := splitKeywords("война, мир, классика")
	if len(got) != 3 || got[2] != "классика" {
		t.Errorf("comma keywords = %v", got)
	}
	got = splitKeywords("a:b")
	if len(got) != 2 {
		t.Errorf("colon keywords = %v", got)
	}
}
