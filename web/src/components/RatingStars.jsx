import "./RatingStars.css";

const FULL = "★";
const EMPTY = "☆";

const clamp = (value, min, max) => Math.min(Math.max(value, min), max);

const RatingStars = ({
  value = 0,
  max = 5,
  showNumeric = true,
  count,
  size = "md",
  ariaLabel,
}) => {
  const numeric = Number(value) || 0;
  const filled = clamp(Math.round(numeric * 2) / 2, 0, max);
  const fillPercent = (filled / max) * 100;

  const stars = (
    <span className="rating-stars__layer" aria-hidden="true">
      {Array.from({ length: max }).map((_, idx) => (
        <span key={idx}>{EMPTY}</span>
      ))}
    </span>
  );

  return (
    <span
      className={`rating-stars rating-stars--${size}`}
      role="img"
      aria-label={ariaLabel ?? `${filled} / ${max}`}
    >
      <span className="rating-stars__track">
        {stars}
        <span
          className="rating-stars__filled"
          style={{ width: `${fillPercent}%` }}
        >
          <span className="rating-stars__layer" aria-hidden="true">
            {Array.from({ length: max }).map((_, idx) => (
              <span key={idx}>{FULL}</span>
            ))}
          </span>
        </span>
      </span>
      {showNumeric && filled > 0 && (
        <span className="rating-stars__numeric">{filled.toFixed(1).replace(".0", "")}</span>
      )}
      {typeof count === "number" && count > 0 && (
        <span className="rating-stars__count">· {count.toLocaleString("ru-RU")}</span>
      )}
    </span>
  );
};

export default RatingStars;
