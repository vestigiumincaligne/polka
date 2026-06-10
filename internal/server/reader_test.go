package server

import (
	"encoding/json"
	"strings"
	"testing"
)

const readerFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<description><title-info><book-title>Читалка</book-title>
<author><first-name>Тест</first-name><last-name>Авторов</last-name></author>
</title-info></description>
<body>
<section><title><p>Глава 1</p></title><p>Текст первой главы со сноской<a l:href="#z1" type="note">[1]</a>.</p></section>
<section><title><p>Глава 2</p></title><p>Текст второй главы.</p></section>
</body>
<body name="notes"><section id="z1"><p>Сноска.</p></section></body>
</FictionBook>`

func TestReaderEndpoints(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"reader.fb2": []byte(readerFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	bookID := itoa64(up["results"][0].BookID)

	// Table of contents
	var meta map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/read/" + bookID)
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("read meta -> %d", resp.StatusCode)
	}
	chapters := meta["chapters"].([]any)
	if len(chapters) != 2 {
		t.Fatalf("chapters = %d, want 2", len(chapters))
	}
	if chapters[0].(map[string]any)["title"] != "Глава 1" {
		t.Errorf("toc[0] = %v", chapters[0])
	}
	notes := meta["notes"].(map[string]any)
	if !strings.Contains(notes["z1"].(string), "Сноска.") {
		t.Errorf("notes = %v", notes)
	}

	// Chapter
	var ch map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/read/" + bookID + "/chapter/1")
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	if !strings.Contains(ch["html"].(string), "Текст второй главы.") || ch["total"].(float64) != 2 {
		t.Errorf("chapter = %v", ch)
	}
	resp, _ = client.Get(ts.URL + "/api/v1/read/" + bookID + "/chapter/9")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("bad chapter -> %d", resp.StatusCode)
	}

	// Progress: saving and reading
	resp = postJSON(t, client, ts.URL+"/api/v1/read/"+bookID+"/progress",
		map[string]any{"chapter": 1, "position": 0.42})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("save progress -> %d", resp.StatusCode)
	}
	var prog map[string]any
	resp, _ = client.Get(ts.URL + "/api/v1/read/" + bookID + "/progress")
	json.NewDecoder(resp.Body).Decode(&prog)
	resp.Body.Close()
	if prog["chapter"].(float64) != 1 || prog["position"].(float64) != 0.42 {
		t.Errorf("progress = %v", prog)
	}
}
