# Book collections

A collection is a JSON file with an "author + title" list. Polka loads it,
finds the listed books in the library and shows what it found as a separate
shelf on the home page (collections without a single match are not shown).
The full list, with marks for what is missing, is served by `GET /api/v1/collections/<slug>`.

The files in this directory are **embedded into the binary** and load
automatically at server start (updating Polka updates them too). A bundled
collection deleted by the administrator does not come back on the next start.
Your own collections can be uploaded under **Management → Book collections** or with

```sh
polka collections import collections/russian-classics.json   # add/update
polka collections list                                        # what is loaded
polka collections match                                       # recompute after a catalog import
polka collections remove russian-classics
```

Matching is recomputed automatically at server start and after every inpx
import. Collections are stored in `collections.db` next to the database and
survive `import --replace`.

## Bundled collections

| File | Collection | Source |
|---|---|---|
| `le-monde-100.json` | 100 книг века по версии Le Monde | [Wikipedia](https://ru.wikipedia.org/wiki/100_книг_века_по_версии_Le_Monde) |
| `bbc-big-read.json` | 200 лучших романов по версии BBC (The Big Read) | [Wikipedia](https://ru.wikipedia.org/wiki/200_лучших_романов_по_версии_Би-би-си) |
| `verdensbiblioteket.json` | 100 лучших книг всех времён (Норвежский книжный клуб) | [Wikipedia](https://ru.wikipedia.org/wiki/Всемирная_библиотека_(Норвежский_книжный_клуб)) |
| `100-knig-shkolnikam.json` | 100 книг для школьников (Минобрнауки) | [Wikipedia](https://ru.wikipedia.org/wiki/100_книг_для_школьников) |
| `russian-classics.json` | Русская классика: с чего начать | example |

The lists were scraped from Wikipedia tables with a script; to add your own,
put a JSON file here and rebuild, or upload it through the UI.

## Format

```json
{
  "slug": "forbes-100",
  "title": "100 книг по версии Forbes",
  "description": "Необязательное описание подборки.",
  "source": "Forbes",
  "url": "https://…",
  "items": [
    {"title": "1984", "author": "Джордж Оруэлл", "isbn": "9785170800902", "year": 1949, "note": "…"},
    "Михаил Булгаков — Мастер и Маргарита",
    "Толстой Л. Н.: Война и мир"
  ]
}
```

- `slug` is the identifier (Latin/Cyrillic letters, digits, hyphen); if not
  set, it is derived from `title`. Re-importing a file with the same `slug`
  replaces the list.
- `items` are objects or strings `Author — Title` (separators: «—», «–»,
  «--», «: », «. »). Only `title` is required; `isbn` gives an exact match
  if the book in the library is indexed by ISBN.
- The author may be in any order («Имя Фамилия», «Фамилия Имя», «Фамилия, Имя»);
  matching goes by last name with declensions taken into account; the title
  is matched entirely, without the subtitle (`Title: subtitle`, `Title (novel)`)
  or by prefix («Война и мир» finds «Война и мир. Том 1»).

## External sources

Polka can scrape collections from websites on its own (package
`internal/collections/sources`). Sources are **disabled by default** and are
enabled under "Management → Book collections"; an enabled source is crawled
immediately and then once a day; only titles, authors and the article link
are taken. Collections of a disabled source stay in the database but
disappear from the home page; a source collection deleted by the administrator
is not restored.

- **Forbes.ru** — articles tagged [«Подборки книг»](https://www.forbes.ru/tegi/podborki-knig):
  each article ("seven books on managing stress") is a separate collection
  of 5–10 books. The tag page serves only the latest ~12 articles, so
  collections accumulate over time. Parsing depends on the site's markup and
  may break; in that case the error of the last crawl is shown in settings.

## Where to get lists

- **Wikidata / Wikipedia** — awards (Booker, Nobel, Hugo, «Большая книга»),
  «100 книг века», «100 книг для школьников»; a SPARQL query on `P166`
  (award received) with Russian labels gives ready-made author + title pairs.
- **Open Library** — `https://openlibrary.org/subjects/<topic>.json`, with ISBNs.
- **FantLab API**, **LiveLib**, **polka.academy** («108 главных русских книг»),
  editorial lists from Forbes / Esquire / «Горький» — transferred to JSON
  manually or with a script.
