package library

import (
	"reflect"
	"testing"
)

func TestGenresFromSubjects(t *testing.T) {
	got := GenresFromSubjects([]string{
		"Science fiction", "Detective and mystery stories", "England -- Fiction",
	})
	want := []string{"sf", "detective", "prose_classic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if g := GenresFromSubjects([]string{"Cooking"}); len(g) != 0 {
		t.Errorf("unmapped subject -> %v", g)
	}
}
