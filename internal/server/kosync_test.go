package server

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// kosyncDo sends a request the way kosync.koplugin does: JSON body,
// x-auth-* headers, the vendored Accept header.
func kosyncDo(t *testing.T, base, method, path, user, key, body string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("accept", "application/vnd.koreader.v1+json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("x-auth-user", user)
		req.Header.Set("x-auth-key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestKosyncProtocol(t *testing.T) {
	f := newFullServer(t, "required")
	base := f.ts.URL
	devKey := md5hex("device-pass-123")

	if code, body := kosyncDo(t, base, "GET", "/healthcheck", "", "", ""); code != 200 || body["state"] != "OK" {
		t.Errorf("healthcheck: %d %v", code, body)
	}
	// Device-side registration is refused with the reference error shape.
	code, body := kosyncDo(t, base, "POST", "/users/create", "", "", `{"username":"reader","password":"`+devKey+`"}`)
	if code != http.StatusPaymentRequired || body["code"].(float64) != 2005 || body["message"] == "" {
		t.Errorf("create: %d %v", code, body)
	}
	// No key configured yet: auth fails.
	if code, body := kosyncDo(t, base, "GET", "/users/auth", "reader", devKey, ""); code != 401 || body["code"].(float64) != 2001 {
		t.Errorf("auth before setup: %d %v", code, body)
	}

	// The user enables sync in the web UI with a device password.
	if resp := postJSON(t, f.reader, base+"/api/v1/me/kosync", map[string]string{"password": "short"}); resp.StatusCode != 400 {
		t.Errorf("weak device password -> %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	var st struct {
		Enabled bool   `json:"enabled"`
		Login   string `json:"login"`
	}
	decode(t, postJSON(t, f.reader, base+"/api/v1/me/kosync", map[string]string{"password": "device-pass-123"}), &st)
	if !st.Enabled || st.Login != "reader" {
		t.Fatalf("enable: %+v", st)
	}

	if code, body := kosyncDo(t, base, "GET", "/users/auth", "reader", devKey, ""); code != 200 || body["authorized"] != "OK" {
		t.Errorf("auth: %d %v", code, body)
	}
	if code, _ := kosyncDo(t, base, "GET", "/users/auth", "reader", md5hex("wrong"), ""); code != 401 {
		t.Errorf("wrong key must fail")
	}
	if code, _ := kosyncDo(t, base, "GET", "/users/auth", "admin", devKey, ""); code != 401 {
		t.Errorf("another user's key must not work")
	}

	// Push a position and read it back.
	doc := strings.Repeat("a", 32)
	code, body = kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey,
		`{"document":"`+doc+`","progress":"/body/DocFragment[7]/body/p[2]","percentage":0.42,"device":"PocketBook","device_id":"DEV1"}`)
	if code != 200 || body["document"] != doc || body["timestamp"] == nil {
		t.Fatalf("update: %d %v", code, body)
	}
	code, body = kosyncDo(t, base, "GET", "/syncs/progress/"+doc, "reader", devKey, "")
	if code != 200 || body["progress"] != "/body/DocFragment[7]/body/p[2]" || body["percentage"].(float64) != 0.42 ||
		body["device"] != "PocketBook" || body["device_id"] != "DEV1" || body["timestamp"] == nil {
		t.Errorf("get: %d %v", code, body)
	}
	// A paged document reports the page as a bare number.
	code, _ = kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey,
		`{"document":"`+strings.Repeat("b", 32)+`","progress":42,"percentage":0.1,"device":"PB"}`)
	if code != 200 {
		t.Errorf("numeric progress: %d", code)
	}
	if _, body = kosyncDo(t, base, "GET", "/syncs/progress/"+strings.Repeat("b", 32), "reader", devKey, ""); body["progress"] != "42" {
		t.Errorf("numeric progress echo: %v", body)
	}
	// Unknown document: 200 with an empty object.
	if code, body = kosyncDo(t, base, "GET", "/syncs/progress/"+strings.Repeat("c", 32), "reader", devKey, ""); code != 200 || len(body) != 0 {
		t.Errorf("unknown doc: %d %v", code, body)
	}
	// Malformed requests match the reference error codes.
	if code, body = kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey, `{"document":"../etc","progress":"x","percentage":0.1,"device":"d"}`); code != 403 || body["code"].(float64) != 2004 {
		t.Errorf("bad document: %d %v", code, body)
	}
	if code, body = kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey, `{"document":"`+doc+`","percentage":0.1}`); code != 403 || body["code"].(float64) != 2003 {
		t.Errorf("missing fields: %d %v", code, body)
	}
	if code, _ = kosyncDo(t, base, "GET", "/syncs/progress/nothex", "reader", devKey, ""); code != 403 {
		t.Errorf("bad doc in get: %d", code)
	}
	if code, _ = kosyncDo(t, base, "PUT", "/syncs/progress", "", "", `{}`); code != 401 {
		t.Errorf("unauthenticated put: %d", code)
	}

	// Device-side password change.
	newKey := md5hex("device-pass-456")
	if code, _ = kosyncDo(t, base, "PUT", "/users/password", "reader", devKey, `{"password":"`+newKey+`"}`); code != 200 {
		t.Fatalf("password change: %d", code)
	}
	if code, _ = kosyncDo(t, base, "GET", "/users/auth", "reader", devKey, ""); code != 401 {
		t.Error("old key must stop working")
	}
	if code, _ = kosyncDo(t, base, "GET", "/users/auth", "reader", newKey, ""); code != 200 {
		t.Error("new key must work")
	}
	// Disable from the web UI.
	postJSON(t, f.reader, base+"/api/v1/me/kosync", map[string]string{"password": ""}).Body.Close()
	if code, _ = kosyncDo(t, base, "GET", "/users/auth", "reader", newKey, ""); code != 401 {
		t.Error("disabled sync must reject the key")
	}
}

func TestKosyncBridgeToShelves(t *testing.T) {
	f := newFullServer(t, "required")
	base := f.ts.URL
	devKey := md5hex("device-pass-123")
	postJSON(t, f.reader, base+"/api/v1/me/kosync", map[string]string{"password": "device-pass-123"}).Body.Close()

	// The web reader already remembers a chapter position.
	postJSON(t, f.reader, base+"/api/v1/read/"+itoa64(f.bookID)+"/progress", map[string]any{"chapter": 3, "position": 0.5, "progress": 0.1}).Body.Close()

	// The device downloads the book through Polka (digest gets recorded)…
	resp := do(t, f.reader, "GET", base+"/Images/fb2/"+itoa64(f.bookID), "")
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != testFB2 {
		t.Fatal("download mismatch")
	}
	doc := referenceDigest(data)

	// …and pushes its reading position.
	if code, _ := kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey,
		`{"document":"`+doc+`","progress":"/body/p[1]","percentage":0.66,"device":"PocketBook"}`); code != 200 {
		t.Fatal("push failed")
	}

	// The overall progress follows the device; the chapter is preserved.
	prog := getJSONWith(t, f.reader, base+"/api/v1/read/"+itoa64(f.bookID)+"/progress")
	if prog["progress"].(float64) != 0.66 || prog["chapter"].(float64) != 3 || prog["position"].(float64) != 0.5 {
		t.Errorf("bridged progress: %v", prog)
	}
	home := getJSONWith(t, f.reader, base+"/main/getBooks/getHomeShelves")
	found := false
	for _, sh := range home["shelves"].([]any) {
		m := sh.(map[string]any)
		if m["id"] == "reading" {
			found = true
			if rp := m["books"].([]any)[0].(map[string]any)["ReadingProgress"].(float64); rp != 0.66 {
				t.Errorf("reading shelf progress: %v", rp)
			}
		}
	}
	if !found {
		t.Error("reading shelf missing")
	}

	// An unknown digest syncs fine but touches no book.
	if code, _ := kosyncDo(t, base, "PUT", "/syncs/progress", "reader", devKey,
		`{"document":"`+strings.Repeat("d", 32)+`","progress":"x","percentage":0.9,"device":"PB"}`); code != 200 {
		t.Error("unknown digest must still sync")
	}
	prog = getJSONWith(t, f.reader, base+"/api/v1/read/"+itoa64(f.bookID)+"/progress")
	if prog["progress"].(float64) != 0.66 {
		t.Errorf("unrelated digest must not move the book: %v", prog)
	}
}

func TestKosyncAuthRateLimit(t *testing.T) {
	f := newFullServer(t, "required")
	for i := 0; i < 10; i++ {
		kosyncDo(t, f.ts.URL, "GET", "/users/auth", "reader", md5hex("guess"), "")
	}
	postJSON(t, f.reader, f.ts.URL+"/api/v1/me/kosync", map[string]string{"password": "device-pass-123"}).Body.Close()
	if code, _ := kosyncDo(t, f.ts.URL, "GET", "/users/auth", "reader", md5hex("device-pass-123"), ""); code != 401 {
		t.Errorf("blocked window must reject even the right key, got %d", code)
	}
}
