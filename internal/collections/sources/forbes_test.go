package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const forbesTag = `<html><body>
<a href="/forbes-woman/553881-bez-vygorania-sem-knig">x</a>
<a href="/page/554096-podpiska">skip</a>
<a href="/svoi-biznes/531651-sest-knig-dla-rukovoditelej">y</a>
<a href="/forbes-woman/553881-bez-vygorania-sem-knig">dup</a>
<a href="/society/500000-bez-knig">no books</a>
</body></html>`

const forbesArticle = `<html><body><h1 class="x">Без выгорания: семь книг об управлении стрессом</h1>
<p>текст</p>
<h2 class="-v-Ta">«Выгорание. Новый подход к избавлению от стресса», Эмили Нагоски и Амелия Нагоски</h2>
<h2 class="-v-Ta"> «Внутренняя сила», Кристин Нефф</h2>
<h2>Не книга</h2>
<h2 class="-v-Ta">«Чувство&nbsp;штиля», Лора&nbsp;Вандеркам </h2>
<h2 class="-v-Ta">«Верю / не верю»</h2>
<h2 class="-v-Ta">«Ты»</h2>
</body></html>`

func TestParseForbesArticle(t *testing.T) {
	f := ParseForbesArticle(forbesArticle)
	if f == nil {
		t.Fatal("nil")
	}
	if f.Title != "Без выгорания: семь книг об управлении стрессом" || f.Source != "Forbes" {
		t.Errorf("header: %+v", f)
	}
	if len(f.Items) != 4 {
		t.Fatalf("items: %+v", f.Items)
	}
	if f.Items[3].Title != "Верю / не верю" || f.Items[3].Author != "" {
		t.Errorf("authorless item: %+v", f.Items[3])
	}
	if f.Items[0].Title != "Выгорание. Новый подход к избавлению от стресса" || f.Items[0].Author != "Эмили Нагоски, Амелия Нагоски" {
		t.Errorf("item0: %+v", f.Items[0])
	}
	if f.Items[2].Title != "Чувство штиля" || f.Items[2].Author != "Лора Вандеркам" {
		t.Errorf("item2: %+v", f.Items[2])
	}
	if ParseForbesArticle(`<h1>Одна</h1><h2>«Книга», Автор</h2>`) != nil {
		t.Error("single book should not be a collection")
	}
}

func TestForbesFetch(t *testing.T) {
	hits := map[string]int{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		switch r.URL.Path {
		case "/tegi/podborki-knig":
			w.Write([]byte(forbesTag))
		case "/forbes-woman/553881-bez-vygorania-sem-knig", "/svoi-biznes/531651-sest-knig-dla-rukovoditelej":
			w.Write([]byte(forbesArticle))
		case "/society/500000-bez-knig":
			w.Write([]byte(`<h1>Статья</h1><p>без книг</p>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	src := NewForbes(ts.URL)
	files, err := src.Fetch(context.Background(), map[string]bool{"forbes-531651": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Slug != "forbes-553881" {
		t.Fatalf("files: %+v", files)
	}
	if files[0].URL != "https://www.forbes.ru/forbes-woman/553881-bez-vygorania-sem-knig" {
		t.Errorf("url: %s", files[0].URL)
	}
	if hits["/svoi-biznes/531651-sest-knig-dla-rukovoditelej"] != 0 {
		t.Error("known article must not be fetched")
	}
	if hits["/forbes-woman/553881-bez-vygorania-sem-knig"] != 1 {
		t.Error("article should be fetched once")
	}
}
