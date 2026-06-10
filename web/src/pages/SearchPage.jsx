import { t } from "../i18n";
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import BookCard from "../components/BookCard";
import Shelf from "../components/Shelf";
import {
  fetchCatalogShelves,
  fetchSearchAuthors,
  fetchSearchGenres,
  fetchSearchSeries,
  fetchSearchStats,
  fetchSearchTitles,
} from "../api/fetchBooks";
import "./SearchPage.css";

const SCOPES = () => [
  { id: "bookTitles", label: t("scope.titles"), statKey: "bookTitles" },
  { id: "authors", label: t("scope.authors"), statKey: "authors" },
  { id: "bookSeries", label: t("scope.series"), statKey: "bookSeries" },
  { id: "genres", label: t("scope.genres"), statKey: "genres" },
];

const SearchPage = ({ config }) => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  const query = searchParams.get("q") ?? "";
  const scope = searchParams.get("scope") ?? "bookTitles";
  const groupId = searchParams.get("group") ?? null;

  const [stats, setStats] = useState(null);
  const [results, setResults] = useState([]);
  const [loading, setLoading] = useState(false);

  const enabled = query.trim().length >= 3 || Boolean(groupId);

  useEffect(() => {
    if (!enabled) {
      setResults([]);
      setStats(null);
      return undefined;
    }
    let cancelled = false;
    setLoading(true);
    const params = { search: query, selectedGroupID: groupId };

    fetchSearchStats(params).then((res) => !cancelled && setStats(res?.searchStats ?? null));

    const loaderByScope = {
      bookTitles: fetchSearchTitles,
      authors: fetchSearchAuthors,
      bookSeries: fetchSearchSeries,
      genres: fetchSearchGenres,
    };

    (loaderByScope[scope] ?? fetchSearchTitles)(params)
      .then((res) => {
        if (cancelled) return;
        setResults(res?.titlesList ?? res?.authorsList ?? res?.seriesList ?? res?.genresList ?? []);
      })
      .finally(() => !cancelled && setLoading(false));

    return () => {
      cancelled = true;
    };
  }, [query, scope, groupId, enabled]);

  // Catalog with no query and no group — show random shelves by genre.
  const [catalogShelves, setCatalogShelves] = useState([]);
  const [catalogLoading, setCatalogLoading] = useState(false);

  useEffect(() => {
    if (enabled) return undefined;
    let cancelled = false;
    setCatalogLoading(true);
    fetchCatalogShelves()
      .then((res) => !cancelled && setCatalogShelves(res?.shelves ?? []))
      .catch(() => !cancelled && setCatalogShelves([]))
      .finally(() => !cancelled && setCatalogLoading(false));
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  const setScope = (next) => {
    const params = new URLSearchParams(searchParams);
    params.set("scope", next);
    setSearchParams(params, { replace: true });
  };

  const setGroup = (next) => {
    const params = new URLSearchParams(searchParams);
    if (next) {
      params.set("group", String(next));
    } else {
      params.delete("group");
    }
    setSearchParams(params, { replace: true });
  };

  const headline = useMemo(() => {
    if (groupId && config?.groups) {
      const grp = config.groups.find((g) => String(g.GroupID) === String(groupId));
      if (grp) return t("search.group", { name: grp.Title });
    }
    if (query) return t("search.headline", { q: query });
    return t("search.catalog");
  }, [config, groupId, query]);

  return (
    <div className="container search-page">
      <header className="search-page__header">
        <h1>{headline}</h1>
        {!enabled && (
          <p className="search-page__hint">
            {t("search.hint")}
          </p>
        )}
      </header>

      {config?.groups?.length ? (
        <div className="search-page__groups">
          <button
            type="button"
            className={`search-page__group ${!groupId ? "is-active" : ""}`}
            onClick={() => setGroup(null)}
          >
            {t("search.all")}
          </button>
          {config.groups.map((g) => (
            <button
              type="button"
              key={g.GroupID}
              className={`search-page__group ${
                String(groupId) === String(g.GroupID) ? "is-active" : ""
              }`}
              onClick={() => setGroup(g.GroupID)}
            >
              {g.Title}
              <span className="search-page__group-count">{g.numberOfBooks}</span>
            </button>
          ))}
        </div>
      ) : null}

      {!enabled && (
        <div className="search-page__catalog">
          {catalogLoading && (
            <div className="search-page__loader">{t("search.loadingShelves")}</div>
          )}
          {!catalogLoading && catalogShelves.length === 0 && (
            <div className="search-page__empty">{t("search.noShelves")}</div>
          )}
          {catalogShelves.map((shelf) => (
            <Shelf
              key={shelf.id}
              title={shelf.title}
              books={shelf.books}
              subtitle={shelf.subtitle}
              onSeeAll={
                shelf.hasMore
                  ? () =>
                      navigate(`/shelf/${encodeURIComponent(shelf.id)}`, {
                        state: { title: shelf.title },
                      })
                  : undefined
              }
            />
          ))}
        </div>
      )}

      {enabled && (
        <div className="search-page__scope-tabs">
          {SCOPES().map((s) => (
            <button
              type="button"
              key={s.id}
              className={`search-page__scope ${scope === s.id ? "is-active" : ""}`}
              onClick={() => setScope(s.id)}
            >
              {s.label}
              {stats && (
                <span className="search-page__scope-count">{stats[s.statKey] ?? 0}</span>
              )}
            </button>
          ))}
        </div>
      )}

      {enabled && loading && <div className="search-page__loader">{t("loading")}</div>}

      {enabled && !loading && scope === "bookTitles" && (
        <div className="search-page__grid">
          {results.map((book) => (
            <BookCard key={book.BookID} book={book} />
          ))}
          {results.length === 0 && (
            <div className="search-page__empty">{t("search.noBooks")}</div>
          )}
        </div>
      )}

      {enabled && !loading && scope === "authors" && (
        <ul className="search-page__list">
          {results.map((a) => (
            <li key={a.AuthorID}>
              <Link
                to={`/author/${a.AuthorID}`}
                state={{ title: a.Authors }}
                className="search-page__list-link"
              >
                <span>{a.Authors}</span>
                <span className="tag">{t("books.count", { n: a.Books })}</span>
              </Link>
            </li>
          ))}
          {results.length === 0 && (
            <div className="search-page__empty">{t("search.noAuthors")}</div>
          )}
        </ul>
      )}

      {enabled && !loading && scope === "genres" && (
        <ul className="search-page__list">
          {results.map((g) => (
            <li key={g.GenreCode}>
              <Link
                to={`/shelf/${encodeURIComponent(`genre_${g.GenreCode}`)}`}
                state={{ title: g.GenreName }}
                className="search-page__list-link"
              >
                <span>{g.GenreName}</span>
                <span className="tag">{t("books.count", { n: g.Books })}</span>
              </Link>
            </li>
          ))}
          {results.length === 0 && (
            <div className="search-page__empty">{t("search.noGenres")}</div>
          )}
        </ul>
      )}

      {enabled && !loading && scope === "bookSeries" && (
        <ul className="search-page__list">
          {results.map((s) => (
            <li key={s.SeriesID}>
              <Link
                to={`/series/${s.SeriesID}`}
                state={{ title: s.SeriesTitle }}
                className="search-page__list-link"
              >
                <span>{s.SeriesTitle}</span>
                <span className="tag">{t("books.count", { n: s.Books })}</span>
              </Link>
            </li>
          ))}
          {results.length === 0 && (
            <div className="search-page__empty">{t("search.noSeries")}</div>
          )}
        </ul>
      )}
    </div>
  );
};

export default SearchPage;
