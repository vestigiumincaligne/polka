package server

import (
	"net/http"
	"testing"
)

func TestReaderPrefs(t *testing.T) {
	f := newFullServer(t, "required")
	url := f.ts.URL + "/api/v1/me/reader-prefs"

	if got := code(t, &http.Client{}, "GET", url, ""); got != http.StatusUnauthorized {
		t.Errorf("anonymous -> %d", got)
	}
	// Empty by default.
	if prefs := getJSONWith(t, f.reader, url); len(prefs) != 0 {
		t.Errorf("default prefs: %v", prefs)
	}
	// Partial updates merge; out-of-range steps clamp; junk is rejected.
	postJSON(t, f.reader, url, map[string]any{"theme": "night", "fontStep": 2}).Body.Close()
	postJSON(t, f.reader, url, map[string]any{"font": "sans", "lineStep": 99, "widthStep": -5}).Body.Close()
	prefs := getJSONWith(t, f.reader, url)
	if prefs["theme"] != "night" || prefs["fontStep"].(float64) != 2 ||
		prefs["font"] != "sans" || prefs["lineStep"].(float64) != 2 || prefs["widthStep"].(float64) != 0 {
		t.Errorf("merged prefs: %v", prefs)
	}
	if got := code(t, f.reader, "POST", url, `{"theme":"neon"}`); got != http.StatusBadRequest {
		t.Errorf("bad theme -> %d", got)
	}
	if got := code(t, f.reader, "POST", url, `not json`); got != http.StatusBadRequest {
		t.Errorf("bad body -> %d", got)
	}
	// Per user: the admin has their own settings.
	if prefs := getJSONWith(t, f.admin, url); len(prefs) != 0 {
		t.Errorf("admin must not see reader's prefs: %v", prefs)
	}
}
