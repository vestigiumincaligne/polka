import { t } from "../i18n";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import Shelf from "../components/Shelf";
import { fetchHomeShelves } from "../api/fetchBooks";
import "./HomePage.css";

const SHELF_TITLES = () => ({
  reading: { title: t("shelf.reading"), subtitle: t("shelf.reading.sub") },
  wishlist: { title: t("shelf.wishlist"), subtitle: t("shelf.wishlist.sub") },
  offline: { title: t("shelf.offline"), subtitle: t("shelf.offline.sub") },
  series_next: { title: t("shelf.series_next"), subtitle: t("shelf.series_next.sub") },
  for_you: { title: t("shelf.for_you"), subtitle: t("shelf.for_you.sub") },
  newest: { title: t("shelf.newest"), subtitle: t("shelf.newest.sub") },
  top_rated: { title: t("shelf.top_rated"), subtitle: t("shelf.top_rated.sub") },
  well_curated: { title: t("shelf.well_curated"), subtitle: t("shelf.well_curated.sub") },
});

const HomePage = ({ config }) => {
  const navigate = useNavigate();
  const [shelves, setShelves] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetchHomeShelves()
      .then((res) => {
        if (cancelled) return;
        setShelves(res?.shelves ?? []);
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err);
        setShelves([]);
      })
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="container homepage">
      <section className="hero">
        <div className="hero__content">
          <p className="hero__eyebrow">{t("home.eyebrow")}</p>
          <h1 className="hero__title">
            {config?.numberOfBooks
              ? t("home.titleCount", { n: config.numberOfBooks.toLocaleString() })
              : t("home.title")}
          </h1>
          <p className="hero__lead">
{t("home.lead")}
          </p>
          <div className="hero__actions">
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => navigate("/search?scope=bookTitles")}
            >
              {t("home.openCatalog")}
            </button>
            {config?.groups?.length ? (
              <span className="hero__hint">
                {t("home.collections")} <b>{config.groups.length}</b>
              </span>
            ) : null}
          </div>
        </div>
        <div className="hero__deco" aria-hidden="true">
          <div className="hero__deco-card hero__deco-card--1" />
          <div className="hero__deco-card hero__deco-card--2" />
          <div className="hero__deco-card hero__deco-card--3" />
        </div>
      </section>

      {loading && (
        <div className="homepage__loading">
          {Array.from({ length: 2 }).map((_, idx) => (
            <div className="homepage__loading-shelf" key={idx}>
              <div className="skeleton homepage__loading-title" />
              <div className="homepage__loading-track">
                {Array.from({ length: 6 }).map((__, i) => (
                  <div className="skeleton homepage__loading-card" key={i} />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {error && !loading && (
        <div className="homepage__error">
{t("home.loadError")}
        </div>
      )}

      {!loading && !error && shelves.length === 0 && (
        <div className="homepage__error">
{t("home.empty")}
        </div>
      )}

      <div className="homepage__shelves">
        {shelves.map((shelf) => {
          const meta = SHELF_TITLES()[shelf.id] ?? { title: shelf.title || shelf.id };
          return (
            <Shelf
              key={shelf.id}
              title={meta.title}
              subtitle={meta.subtitle}
              books={shelf.books}
              onSeeAll={
                shelf.id === "wishlist"
                  ? () => navigate("/lists")
                  : shelf.hasMore
                    ? () =>
                        navigate(`/shelf/${encodeURIComponent(shelf.id)}`, {
                          state: { title: meta.title },
                        })
                    : undefined
              }
            />
          );
        })}
      </div>
    </div>
  );
};

export default HomePage;
