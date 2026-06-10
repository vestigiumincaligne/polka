import { t } from "../i18n";
import { useState } from "react";
import "./RateStars.css";

// Interactive stars: a click sets the rating, clicking the current one clears it.
const RateStars = ({ value = 0, onRate, disabled = false }) => {
  const [hover, setHover] = useState(0);
  const shown = hover || value;

  return (
    <div className="rate-stars" onMouseLeave={() => setHover(0)} role="radiogroup" aria-label={t("book.rate.yours")}>
      {[1, 2, 3, 4, 5].map((n) => (
        <button
          key={n}
          type="button"
          className={`rate-stars__star ${n <= shown ? "is-filled" : ""} ${hover > 0 ? "is-hovering" : ""}`}
          disabled={disabled}
          onMouseEnter={() => setHover(n)}
          onFocus={() => setHover(n)}
          onClick={() => onRate(n === value ? 0 : n)}
          aria-label={`${n} / 5`}
          title={`${n} / 5`}
        >
          ★
        </button>
      ))}
    </div>
  );
};

export default RateStars;
