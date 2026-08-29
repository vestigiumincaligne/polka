package genres

import (
	"strings"
	"testing"
)

func TestNameAndNameLang(t *testing.T) {
	if Name("sf") != "Научная фантастика" {
		t.Errorf("Name(sf) = %q", Name("sf"))
	}
	if Name("no_such_genre") != "no_such_genre" {
		t.Error("unknown codes are returned as is")
	}
	if got := NameLang("sf", "en"); got == "" || got == "sf" || got == Name("sf") {
		t.Errorf("NameLang(sf, en) = %q", got)
	}
	if NameLang("sf", "ru") != Name("sf") || NameLang("sf", "de") != Name("sf") {
		t.Error("any language but en falls back to Russian")
	}
	if NameLang("no_such_genre", "en") != "no_such_genre" {
		t.Error("unknown code in English is returned as is")
	}
}

// Both tables must describe the same set of codes with non-empty names:
// a code that is localized in one language only would show up as a raw
// code after switching the UI language.
func TestTablesAreConsistent(t *testing.T) {
	if len(names) < 100 {
		t.Errorf("suspiciously few genres: %d", len(names))
	}
	for code, name := range names {
		if strings.TrimSpace(name) == "" {
			t.Errorf("empty Russian name for %q", code)
		}
		if en, ok := namesEN[code]; !ok || strings.TrimSpace(en) == "" {
			t.Errorf("no English name for %q (%s)", code, name)
		}
	}
	for code := range namesEN {
		if _, ok := names[code]; !ok {
			t.Errorf("English-only genre %q", code)
		}
	}
}
