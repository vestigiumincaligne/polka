package store

import (
	"context"
	"path/filepath"
	"testing"
)

func newMatchStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "match.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	session, err := st.NewImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, last, first string, rate float64) {
		if err := session.Add(&BookInput{
			Title: title, Authors: []AuthorName{{Last: last, First: first}},
			Folder: "x.zip", File: title, Ext: "fb2", Lang: "ru", Rate: rate, Added: "2024-01-01",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("1984", "Оруэлл", "Джордж", 5)
	add("1984. Скотный двор", "Оруэлл", "Джордж", 4)
	add("Мастер и Маргарита", "Булгаков", "Михаил", 5)
	add("Война и мир. Том 1", "Толстой", "Лев", 3)
	add("Война и мир. Том 2", "Толстой", "Лев", 5)
	add("Собачье сердце", "Булгаков", "Михаил", 4)
	add("Убить пересмешника", "Ли", "Харпер", 5)
	add("Вишневый сад", "Чехов", "Антон", 5)
	add("Вишневый сад. Повести", "Чехов", "Антон", 5)
	if err := session.Add(&BookInput{
		Title: "Двенадцать стульев", Authors: []AuthorName{{Last: "Ильф", First: "Илья"}, {Last: "Петров", First: "Евгений"}},
		Folder: "x.zip", File: "12", Ext: "fb2", Lang: "ru", Rate: 5, Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Add(&BookInput{
		Title: "Стихотворения и поэмы", Authors: []AuthorName{{Last: "Бодлер"}, {Last: "Гюго"}, {Last: "Симонов", First: "Иван"}, {Last: "Арагон"}},
		Folder: "x.zip", File: "anth", Ext: "fb2", Lang: "ru", Rate: 5, Added: "2024-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMatchBookExt(t *testing.T) {
	st := newMatchStore(t)
	ctx := context.Background()
	cases := []struct {
		title, author, wantTitle, wantKind string
	}{
		{"1984", "Джордж Оруэлл", "1984", MatchExact},
		{"1984", "Оруэлл Дж.", "1984", MatchExact},
		{"Мастер и Маргарита (роман)", "Булгаков, Михаил", "Мастер и Маргарита", MatchExact},
		{"Война и мир", "Лев Толстой", "Война и мир. Том 2", MatchFuzzy}, // closest by length, higher rating
		{"Убить пересмешника", "Харпер Ли", "Убить пересмешника", MatchExact},
		{"Собачье сердце: повесть", "Михаил Булгаков", "Собачье сердце", MatchExact},
		{"Вишнёвый сад", "Антон Чехов", "Вишневый сад", MatchExact}, // ё/е in FTS
		{"Двенадцать стульев", "Илья Ильф, Евгений Петров", "Двенадцать стульев", MatchExact},
		{"Двенадцать стульев", "Ильф, Илья", "Двенадцать стульев", MatchExact},
	}
	for _, c := range cases {
		b, kind, err := st.MatchBookExt(ctx, c.title, c.author, "")
		if err != nil {
			t.Errorf("%q / %q: %v", c.title, c.author, err)
			continue
		}
		if b.Title != c.wantTitle || kind != c.wantKind {
			t.Errorf("%q / %q: got %q (%s), want %q (%s)", c.title, c.author, b.Title, kind, c.wantTitle, c.wantKind)
		}
	}
	// A different author is not a match.
	if _, _, err := st.MatchBookExt(ctx, "1984", "Лев Толстой", ""); err == nil {
		t.Error("expected no match for 1984 by Толстой")
	}
	if b, _, err := st.MatchBookExt(ctx, "Стихотворения", "Константин Симонов", ""); err == nil {
		t.Errorf("anthology should not fuzzy-match a namesake: %q", b.Title)
	}
	if _, _, err := st.MatchBookExt(ctx, "Нет такой книги", "", ""); err == nil {
		t.Error("expected no match for unknown title")
	}
}

func TestMatchBookExtISBN(t *testing.T) {
	st := newMatchStore(t)
	ctx := context.Background()
	books, _ := st.SearchTitles(ctx, "Собачье", 1)
	st.SetBookISBN(ctx, books[0].ID, "978-5-17-080090-2")
	b, kind, err := st.MatchBookExt(ctx, "Совсем другое название", "Кто-то", "9785170800902")
	if err != nil || kind != MatchISBN || b.ID != books[0].ID {
		t.Fatalf("isbn match: %v %s %+v", err, kind, b)
	}
}
