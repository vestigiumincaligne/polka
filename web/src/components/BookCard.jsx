import { t } from "../i18n";
import { useState } from "react";
import { Link } from "react-router-dom";
import api from "../api/api";
import RatingStars from "./RatingStars";
import "./BookCard.css";

const BookCard = ({ book, size = "md", layout = "grid" }) => {
  const [coverFailed, setCoverFailed] = useState(false);

  if (!book) return null;

  const { BookID, Title, AuthorsNames, Authors, SeriesTitle, SeqNumber, Year, LibRate, ReadingProgress } = book;

  const initials = (Title || "").trim().slice(0, 2).toUpperCase();
  const seriesLabel =
    SeriesTitle && SeqNumber
      ? `${SeriesTitle} · ${SeqNumber}`
      : SeriesTitle || (Year ? `${Year}` : "");

  return (
    <article className={`book-card book-card--${size} book-card--${layout}`}>
      <Link to={`/book/${BookID}`} className="book-card__cover-link" aria-label={Title}>
        <div className="book-card__cover">
          {coverFailed ? (
            <div className="book-card__cover-fallback">
              <span>{initials || "?"}</span>
            </div>
          ) : (
            <img
              src={api.coverUrl(BookID)}
              alt=""
              loading="lazy"
              onError={() => setCoverFailed(true)}
            />
          )}
          {LibRate > 0 && (
            <div className="book-card__rating-badge">
              <RatingStars value={LibRate} size="sm" showNumeric={false} />
            </div>
          )}
          {typeof ReadingProgress === "number" && (
            <div className="book-card__progress" title={`${Math.round(ReadingProgress * 100)}%`}>
              <div
                className="book-card__progress-fill"
                style={{ width: `${Math.max(2, Math.round(ReadingProgress * 100))}%` }}
              />
            </div>
          )}
        </div>
      </Link>

      <div className="book-card__meta">
        <Link to={`/book/${BookID}`} className="book-card__title" title={Title}>
          {Title}
        </Link>
        {(AuthorsNames || (Array.isArray(Authors) && Authors.length > 0)) && (
          <div className="book-card__author" title={AuthorsNames}>
            {Array.isArray(Authors) && Authors.length > 0
              ? Authors.map((author, index) => (
                  <span key={author.AuthorID}>
                    {index > 0 && ", "}
                    {author.AuthorID
                      ? <Link to={`/author/${author.AuthorID}`}>{author.Name}</Link>
                      : author.Name}
                  </span>
                ))
              : AuthorsNames}
          </div>
        )}
        {seriesLabel && <div className="book-card__series">{seriesLabel}</div>}
      </div>
    </article>
  );
};

export default BookCard;
