import { t } from "../i18n";
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import BookCard from "../components/BookCard";
import { fetchAuthorBooks, fetchSeriesBooks, fetchShelfBooks } from "../api/fetchBooks";
import { exportUrl } from "../api/manage";
import "./BookListPage.css";

const FETCHERS = () => ({
  author: { fetch: fetchAuthorBooks, fallbackTitle: t("scope.authors"), subtitle: t("scope.authors") },
  series: { fetch: fetchSeriesBooks, fallbackTitle: t("scope.series"), subtitle: t("scope.series") },
  shelf: { fetch: fetchShelfBooks, fallbackTitle: t("nav.catalog"), subtitle: t("nav.catalog") },
});

const BookListPage = ({ kind }) => {
  const params = useParams();
  const id =
    kind === "author" ? params.authorId : kind === "series" ? params.seriesId : params.shelfId;
  const location = useLocation();
  const navigate = useNavigate();

  const passedTitle = location.state?.title;
  const config = FETCHERS()[kind];

  const [books, setBooks] = useState([]);
  const [resolvedTitle, setResolvedTitle] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [nextOffset, setNextOffset] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!id) return undefined;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setResolvedTitle("");
    setHasMore(false);
    setNextOffset(0);
    const params = kind === "shelf" ? { shelfId: id } : { selectedItemID: id };
    config
      .fetch(params)
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
  }, [id, config]);

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
        <p className="booklist-page__eyebrow">{config.subtitle}</p>
        <h1 className="booklist-page__title">{passedTitle || resolvedTitle || config.fallbackTitle}</h1>
        {!loading && books.length > 0 && (
          <p className="booklist-page__count">
            {t("books.count", { n: books.length })}
            {books.length <= 500 && (
              <>
                {" · "}
                <a className="root-footer__link" href={exportUrl(books.map((b) => b.BookID))}>
                  {t("downloadAll")}
                </a>
              </>
            )}
          </p>
        )}
      </header>

      {loading && <div className="booklist-page__loader">{t("loading")}</div>}

      {error && !loading && (
        <div className="booklist-page__error">{t("lists.loadFail")}</div>
      )}

      {!loading && !error && books.length === 0 && (
        <div className="booklist-page__error">
          {t("search.noBooks")}{" "}
          <Link to="/" className="root-footer__link">
            {t("book.toHome")}
          </Link>
        </div>
      )}

      {!loading && books.length > 0 && (
        <>
          <div className="booklist-page__grid">
            {sortedBooks.map((book) => (
              <BookCard key={book.BookID} book={book} />
            ))}
          </div>
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
