package inpx

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInpx builds an inpx (a zip) from the given entries.
func writeInpx(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.inpx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(data))
	}
	zw.Close()
	f.Close()
	return path
}

func line(fields ...string) string {
	sep := string(rune(fieldsSeparator))
	return strings.Join(fields, sep) + sep + "\r\n"
}

func TestOpenAndRecords(t *testing.T) {
	// Custom structure with FOLDER, collection.info with a BOM and CRLF,
	// two .inp files, one record overriding the folder, one broken line.
	path := writeInpx(t, map[string]string{
		"structure.info":  "AUTHOR;GENRE;TITLE;SERIES;SERNO;FILE;SIZE;LIBID;DEL;EXT;DATE;LANG;FOLDER;\r\n",
		"collection.info": "\uFEFFМоя коллекция\r\nОписание в две\r\nстроки\r\n",
		"version.info":    "20240101\r\n",
		"arch-001.inp": line("Толстой,Лев,", "prose_classic", "Война и мир", "", "", "100", "10", "1", "0", "fb2", "2024-01-01", "ru", "") +
			line("Чехов,Антон,", "dramaturgy", "Вишневый сад", "", "", "101", "20", "2", "1", "fb2", "2024-01-02", "ru", "custom-folder.zip"),
		"arch-002.inp": "garbage line without separators\n" +
			line("Лем,Станислав,", "sf", "Солярис", "Цикл", "3", "200", "30", "3", "0", "epub", "2024-01-03", "ru", ""),
		"README.txt": "ignored",
	})
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.CollectionName() != "Моя коллекция" {
		t.Errorf("name: %q", f.CollectionName())
	}
	if f.CollectionDescription() != "Описание в две\nстроки" {
		t.Errorf("description: %q", f.CollectionDescription())
	}

	var recs []*Record
	if err := f.Records(func(r *Record) error {
		recs = append(recs, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records: %d", len(recs))
	}
	if r := recs[0]; r.Title != "Война и мир" || r.Folder != "arch-001.zip" || r.File != "100" || r.Ext != "fb2" || r.Deleted {
		t.Errorf("default folder from the .inp name: %+v", r)
	}
	if r := recs[1]; r.Folder != "custom-folder.zip" || !r.Deleted {
		t.Errorf("FOLDER field wins, DEL parsed: %+v", r)
	}
	if r := recs[2]; r.Folder != "arch-002.zip" || r.Series != "Цикл" || r.SeriesNum != 3 || r.Ext != "epub" {
		t.Errorf("second inp: %+v", r)
	}

	// The callback's error stops iteration and names the .inp file.
	stop := errors.New("stop")
	n := 0
	err = f.Records(func(r *Record) error {
		n++
		return stop
	})
	if !errors.Is(err, stop) || !strings.Contains(err.Error(), "arch-001.inp") || n != 1 {
		t.Errorf("error propagation: %v (n=%d)", err, n)
	}
}

func TestOpenDefaultsAndErrors(t *testing.T) {
	// No structure.info → the default field order; no collection.info → empty name.
	path := writeInpx(t, map[string]string{
		"only.inp": line("Автор,Имя,", "sf", "Книга", "", "", "5", "1", "9", "0", "fb2", "2024-01-01", "ru", "0", "", "", ""),
	})
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var got *Record
	f.Records(func(r *Record) error { got = r; return nil })
	f.Close()
	if f.CollectionName() != "" || got == nil || got.Title != "Книга" || got.Folder != "only.zip" {
		t.Errorf("defaults: name=%q rec=%+v", f.CollectionName(), got)
	}

	// An empty structure.info also means the default structure.
	path = writeInpx(t, map[string]string{"structure.info": "  \r\n", "a.inp": line("Автор,,", "sf", "Книга", "", "", "5", "1", "9", "0", "fb2", "2024-01-01", "ru", "0", "", "", "")})
	if f, err := Open(path); err != nil {
		t.Errorf("blank structure.info: %v", err)
	} else {
		f.Records(func(r *Record) error {
			if r.Title != "Книга" {
				t.Errorf("record with default structure: %+v", r)
			}
			return nil
		})
		f.Close()
	}

	if _, err := Open(writeInpx(t, map[string]string{"collection.info": "x"})); err == nil || !strings.Contains(err.Error(), "no .inp") {
		t.Errorf("inpx without .inp entries: %v", err)
	}
	notZip := filepath.Join(t.TempDir(), "x.inpx")
	os.WriteFile(notZip, []byte("nope"), 0o644)
	if _, err := Open(notZip); err == nil {
		t.Error("not a zip must fail")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.inpx")); err == nil {
		t.Error("missing file must fail")
	}
}
