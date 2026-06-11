package library

import "testing"

func TestPathInside(t *testing.T) {
	root := "/var/lib/polka/books"
	cases := map[string]bool{
		"fb2.Flibusta":  true,
		"sub/dir":       true,
		".":             true,
		"":              true,
		"../../../etc":  false,
		"../etc":        false,
		"foo/../../bar": false,
		"/etc/passwd":   true, // absolute joined under root -> books/etc/passwd, still inside
	}
	for rel, want := range cases {
		if got := pathInside(root, rel); got != want {
			t.Errorf("pathInside(%q) = %v, want %v", rel, got, want)
		}
	}
}
