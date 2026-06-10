import { t } from "../i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import BookCard from "./BookCard";
import "./Shelf.css";

const Shelf = ({ title, subtitle, books = [], emptyHint, onSeeAll }) => {
  const trackRef = useRef(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  const updateScrollState = useCallback(() => {
    const el = trackRef.current;
    if (!el) return;
    setCanScrollLeft(el.scrollLeft > 4);
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 4);
  }, []);

  useEffect(() => {
    updateScrollState();
    const el = trackRef.current;
    if (!el) return undefined;
    el.addEventListener("scroll", updateScrollState, { passive: true });
    const ro = new ResizeObserver(updateScrollState);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", updateScrollState);
      ro.disconnect();
    };
  }, [updateScrollState, books.length]);

  const scroll = (direction) => {
    const el = trackRef.current;
    if (!el) return;
    el.scrollBy({ left: direction * el.clientWidth * 0.85, behavior: "smooth" });
  };

  if (!books.length && !emptyHint) return null;

  return (
    <section className="shelf">
      <header className="shelf__header">
        <div>
          <h2 className="shelf__title">{title}</h2>
          {subtitle && <p className="shelf__subtitle">{subtitle}</p>}
        </div>
        <div className="shelf__controls">
          {onSeeAll && (
            <button type="button" className="btn btn-link" onClick={onSeeAll}>
              {t("shelf.seeAll")}
            </button>
          )}
          <div className="shelf__nav">
            <button
              type="button"
              className="shelf__nav-btn"
              aria-label={t("shelf.prev")}
              onClick={() => scroll(-1)}
              disabled={!canScrollLeft}
            >
              ‹
            </button>
            <button
              type="button"
              className="shelf__nav-btn"
              aria-label={t("shelf.next")}
              onClick={() => scroll(1)}
              disabled={!canScrollRight}
            >
              ›
            </button>
          </div>
        </div>
      </header>

      {books.length ? (
        <div className="shelf__track" ref={trackRef}>
          {books.map((book) => (
            <div key={book.BookID} className="shelf__item">
              <BookCard book={book} />
            </div>
          ))}
        </div>
      ) : (
        <div className="shelf__empty">{emptyHint}</div>
      )}
    </section>
  );
};

export default Shelf;
