import { t } from "../i18n";
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import BookCard from "../components/BookCard";
import { fetchAuthorBooks, fetchSeriesBooks, fetchShelfBooks } from "../api/fetchBooks";
import { exportUrl } from "../api/manage";
import { readBookView, writeBookView } from "../bookView";
import "./BookListPage.css";

const FETCHERS = {
  author: fetchAuthorBooks,
  series: fetchSeriesBooks,
  shelf: fetchShelfBooks,
};

const KIND_LABEL = () => ({
  author: t("scope.authors"),
  series: t("scope.series"),
  shelf: t("nav.catalog"),
});

const BookListPage = ({ kind }) => {
  const params = useParams();
  const id =
    kind === "author" ? params.authorId : kind === "series" ? params.seriesId : params.shelfId;
  const location = useLocation();
  const navigate = useNavigate();

  const passedTitle = location.state?.title;
  const fetcher = FETCHERS[kind];
  const label = KIND_LABEL()[kind];

  const [books, setBooks] = useState([]);
  const [resolvedTitle, setResolvedTitle] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [nextOffset, setNextOffset] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [listView, setListView] = useState(() => readBookView() === "list");
  const [groupSeries, setGroupSeries] = useState(true);
  const [language, setLanguage] = useState("");

  useEffect(() => {
    if (!id) return undefined;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setResolvedTitle("");
    setHasMore(false);
    setNextOffset(0);
    const params = kind === "shelf" ? { shelfId: id } : { selectedItemID: id };
    fetcher(params)
      .then((res) => {
        if (cancelled) return;
        setBooks(res?.titlesList ?? []);
        setResolvedTitle(res?.title ?? "");
        if (kind === "shelf") {
          setHasMore(Boolean(res?.hasMore));
          setNextOffset(Number(res?.nextOffset ?? (res?.titlesList?.length ?? 0)));
        }
      })
      .catch((err) => !cancelled && setError(err))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [id, kind]);

  const setView = (list) => {
    setListView(list);
    writeBookView(list ? "list" : "grid");
  };

  const sortedBooks =
    kind === "series"
      ? [...books].sort((a, b) => {
          const ax = parseInt(a.SeqNumber, 10);
          const bx = parseInt(b.SeqNumber, 10);
          if (Number.isNaN(ax) && Number.isNaN(bx)) return 0;
          if (Number.isNaN(ax)) return 1;
          if (Number.isNaN(bx)) return -1;
          return ax - bx;
        })
      : books;

  const languages = [...new Set(books.map((book) => book.Lang).filter(Boolean))].sort();
  const visibleBooks = kind === "author" && language
    ? sortedBooks.filter((book) => book.Lang === language)
    : sortedBooks;
  const groupedBooks = kind === "author" && groupSeries
    ? visibleBooks.reduce((groups, book) => {
        const key = book.SeriesTitle || null;
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key).push(book);
        return groups;
      }, new Map())
    : null;

  useEffect(() => {
    if (!loading && language && !languages.includes(language)) setLanguage("");
  }, [language, languages, loading]);

  const loadMore = () => {
    if (kind !== "shelf" || loadingMore || !hasMore) return;
    setLoadingMore(true);
    fetchShelfBooks({ shelfId: id, offset: nextOffset })
      .then((res) => {
        const chunk = res?.titlesList ?? [];
        setBooks((prev) => [...prev, ...chunk]);
        setHasMore(Boolean(res?.hasMore));
        setNextOffset(Number(res?.nextOffset ?? (nextOffset + chunk.length)));
      })
      .finally(() => setLoadingMore(false));
  };

  return (
    <div className="container booklist-page">
      <button type="button" className="btn btn-link booklist-page__back" onClick={() => navigate(-1)}>
        {t("back")}
      </button>

      <header className="booklist-page__header">
        <p className="booklist-page__eyebrow">{label}</p>
        <h1 className="booklist-page__title">{passedTitle || resolvedTitle || label}</h1>
        {!loading && visibleBooks.length > 0 && (
          <p className="booklist-page__count">
            {t("books.count", { n: visibleBooks.length })}
            {visibleBooks.length <= 500 && (
              <>
                {" · "}
                <a className="root-footer__link" href={exportUrl(visibleBooks.map((b) => b.BookID))}>
                  {t("downloadAll")}
                </a>
              </>
            )}
          </p>
        )}
      </header>

      {!loading && !error && books.length > 0 && (
        <div className="booklist-page__controls">
          <div className="booklist-page__views" role="group" aria-label={t("booklist.view")}>
            <button
              type="button"
              className={`btn ${!listView ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setView(false)}
            >
              {t("booklist.grid")}
            </button>
            <button
              type="button"
              className={`btn ${listView ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setView(true)}
            >
              {t("booklist.list")}
            </button>
          </div>
          {kind === "author" && (
            <>
              <label>
                {t("booklist.language")}
                <select value={language} onChange={(event) => setLanguage(event.target.value)}>
                  <option value="">{t("booklist.allLanguages")}</option>
                  {languages.map((lang) => (
                    <option key={lang} value={lang}>{lang}</option>
                  ))}
                </select>
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={groupSeries}
                  onChange={(event) => setGroupSeries(event.target.checked)}
                />
                {t("booklist.groupSeries")}
              </label>
            </>
          )}
        </div>
      )}

      {loading && <div className="booklist-page__loader">{t("loading")}</div>}

      {error && !loading && (
        <div className="booklist-page__error">{t("lists.loadFail")}</div>
      )}

      {!loading && !error && visibleBooks.length === 0 && (
        <div className="booklist-page__error">
          {t("search.noBooks")}{" "}
          <Link to="/" className="root-footer__link">
            {t("book.toHome")}
          </Link>
        </div>
      )}

      {!loading && visibleBooks.length > 0 && (
        <>
          {groupedBooks
            ? Array.from(groupedBooks, ([series, items]) => (
                <section
                  className="booklist-page__group"
                  key={series === null ? "no-series" : `series:${series}`}
                >
                  <h2>{series ?? t("booklist.noSeries")}</h2>
                  <div className={`booklist-page__grid ${listView ? "booklist-page__grid--list" : ""}`}>
                    {items.map((book) => (
                      <BookCard
                        key={book.BookID}
                        book={book}
                        layout={listView ? "list" : "grid"}
                      />
                    ))}
                  </div>
                </section>
              ))
            : <div className={`booklist-page__grid ${listView ? "booklist-page__grid--list" : ""}`}>
                {visibleBooks.map((book) => (
                  <BookCard
                    key={book.BookID}
                    book={book}
                    layout={listView ? "list" : "grid"}
                  />
                ))}
              </div>}
          {kind === "shelf" && hasMore && (
            <div style={{ display: "flex", justifyContent: "center", marginTop: 20 }}>
              <button type="button" className="btn btn-primary" onClick={loadMore} disabled={loadingMore}>
                {loadingMore ? t("loading") : t("lists.more")}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default BookListPage;
