import { t } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import ePub from "epubjs";
import { fetchProgress, localProgress, saveProgress } from "../api/reader";
import "./ReaderPage.css";
import "./EpubReader.css";

const THEME_STYLES = {
  paper: { body: { color: "#121212", background: "#ffffff" } },
  sepia: { body: { color: "#433422", background: "#f6efe1" } },
  night: { body: { color: "#d9d6d0", background: "#121212" } },
};

const loadTheme = () => {
  try {
    return JSON.parse(localStorage.getItem("polka-reader-prefs"))?.theme ?? "paper";
  } catch {
    return "paper";
  }
};

// EPUB reader built on epub.js: the position is stored as a CFI locator.
const EpubReader = ({ bookId, meta }) => {
  const [error, setError] = useState(null);
  const [ready, setReady] = useState(false);
  const [percent, setPercent] = useState(0);
  const [chapterLabel, setChapterLabel] = useState("");
  const theme = useRef(loadTheme());

  const viewRef = useRef(null);
  const renditionRef = useRef(null);
  const bookRef = useRef(null);
  const serverStored = useRef(false);
  const saveTimer = useRef(null);

  useEffect(() => {
    let cancelled = false;
    const book = ePub(meta.fileUrl, { requestCredentials: true });
    bookRef.current = book;

    const rendition = book.renderTo(viewRef.current, {
      width: "100%",
      height: "100%",
      flow: "scrolled-doc",
    });
    renditionRef.current = rendition;
    rendition.themes.register("polka", {
      body: {
        ...THEME_STYLES[theme.current].body,
        "font-family": 'georgia, "Times New Roman", serif',
        "line-height": "1.7",
        padding: "0 8px",
      },
    });
    rendition.themes.select("polka");

    fetchProgress(bookId)
      .catch(() => null)
      .then((prog) => {
        if (cancelled) return;
        serverStored.current = Boolean(prog?.stored);
        const saved = prog?.stored ? prog : localProgress.get(bookId);
        const cfi = saved?.locator || undefined;
        return rendition.display(cfi);
      })
      .then(() => !cancelled && setReady(true))
      .catch((err) => !cancelled && setError(err));

    // Locations map for the reading-progress percentage
    book.ready
      .then(() => book.locations.generate(600))
      .catch(() => {});

    rendition.on("relocated", (location) => {
      if (cancelled) return;
      const cfi = location?.start?.cfi ?? "";
      let overall = 0;
      try {
        overall = book.locations?.length() ? book.locations.percentageFromCfi(cfi) : 0;
      } catch {
        overall = 0;
      }
      if (!overall && book.spine?.length) {
        overall = (location?.start?.index ?? 0) / book.spine.length;
      }
      setPercent(overall);

      const tocItem = book.navigation?.get(location?.start?.href);
      setChapterLabel(tocItem?.label?.trim() ?? "");

      clearTimeout(saveTimer.current);
      saveTimer.current = setTimeout(() => {
        if (serverStored.current) {
          saveProgress(bookId, location?.start?.index ?? 0, 0, Math.min(1, overall), cfi);
        } else {
          localProgress.set(bookId, location?.start?.index ?? 0, 0);
          try {
            localStorage.setItem(`polka-epub-${bookId}`, cfi);
          } catch {
            /* ignore */
          }
        }
      }, 1200);
    });

    return () => {
      cancelled = true;
      clearTimeout(saveTimer.current);
      book.destroy();
    };
  }, [bookId, meta.fileUrl]);

  const go = (dir) => {
    const r = renditionRef.current;
    if (!r) return;
    if (dir > 0) r.next();
    else r.prev();
  };

  if (error) {
    return (
      <div className="reader reader--paper">
        <div className="reader__error">
          <p>{t("reader.epubFail")}</p>
          <Link to={`/book/${bookId}`} className="btn btn-ghost">{t("reader.back")}</Link>
        </div>
      </div>
    );
  }

  return (
    <div className={`reader reader--${theme.current} epub-reader`}>
      <header className="reader__bar">
        <Link to={`/book/${bookId}`} className="reader__bar-btn" title={t("reader.toBook")}>←</Link>
        <div className="reader__bar-title">
          <span className="reader__bar-book">{meta.title}</span>
          {meta.authors && <span className="reader__bar-author">{meta.authors}</span>}
        </div>
        <button type="button" className="reader__bar-btn" title={t("reader.prevChapter")} onClick={() => go(-1)}>‹</button>
        <button type="button" className="reader__bar-btn" title={t("reader.nextChapter")} onClick={() => go(1)}>›</button>
      </header>

      {!ready && <div className="reader__loading">{t("reader.epubLoading")}</div>}

      <div className="epub-reader__view" ref={viewRef} />

      <footer className="reader__progress" aria-label="progress">
        <div className="reader__progress-fill" style={{ width: `${Math.round(percent * 1000) / 10}%` }} />
        <div className="reader__progress-info">
          <span className="reader__progress-chapter">{chapterLabel || meta.title}</span>
          <span className="reader__progress-pct">{Math.round(percent * 100)}%</span>
        </div>
      </footer>
    </div>
  );
};

export default EpubReader;
