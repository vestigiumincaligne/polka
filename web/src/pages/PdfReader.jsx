import { t } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import * as pdfjsLib from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { fetchProgress, localProgress, saveProgress } from "../api/reader";
import "./ReaderPage.css";
import "./PdfReader.css";

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl;

// PDF reader: a continuous strip of pages rendered while scrolling,
// progress = page number.
const PdfReader = ({ bookId, meta }) => {
  const [doc, setDoc] = useState(null);
  const [numPages, setNumPages] = useState(0);
  const [pageSize, setPageSize] = useState(null); // approximate dimensions for placeholders
  const [current, setCurrent] = useState(1);
  const [error, setError] = useState(null);

  const containerRef = useRef(null);
  const rendered = useRef(new Set());
  const serverStored = useRef(false);
  const restoreTo = useRef(1);
  const saveTimer = useRef(null);

  // Load the document and the saved position
  useEffect(() => {
    let cancelled = false;
    Promise.all([
      pdfjsLib.getDocument({ url: meta.fileUrl, withCredentials: true }).promise,
      fetchProgress(bookId).catch(() => null),
    ])
      .then(async ([pdf, prog]) => {
        if (cancelled) return;
        serverStored.current = Boolean(prog?.stored);
        const saved = prog?.stored ? prog : localProgress.get(bookId);
        restoreTo.current = Math.min(Math.max(1, Number(saved?.chapter ?? 1)), pdf.numPages);

        const page1 = await pdf.getPage(1);
        const vp = page1.getViewport({ scale: 1 });
        if (cancelled) return;
        setPageSize({ width: vp.width, height: vp.height });
        setDoc(pdf);
        setNumPages(pdf.numPages);
      })
      .catch((err) => !cancelled && setError(err));
    return () => {
      cancelled = true;
    };
  }, [bookId, meta.fileUrl]);

  const persist = (page) => {
    const overall = numPages > 0 ? page / numPages : 0;
    if (serverStored.current) {
      saveProgress(bookId, page, 0, overall, "");
    } else {
      localProgress.set(bookId, page, 0);
    }
  };

  // Render a page into the placeholder's canvas
  const renderPage = async (pageNum, holder) => {
    if (rendered.current.has(pageNum) || !doc) return;
    rendered.current.add(pageNum);
    try {
      const page = await doc.getPage(pageNum);
      const width = Math.min(holder.clientWidth, 1200);
      const scale = (width / page.getViewport({ scale: 1 }).width) * (window.devicePixelRatio || 1);
      const viewport = page.getViewport({ scale });
      const canvas = document.createElement("canvas");
      canvas.width = viewport.width;
      canvas.height = viewport.height;
      canvas.style.width = "100%";
      await page.render({ canvasContext: canvas.getContext("2d"), viewport }).promise;
      holder.replaceChildren(canvas);
    } catch {
      rendered.current.delete(pageNum);
    }
  };

  // Lazy rendering + current page tracking
  useEffect(() => {
    if (!doc || !containerRef.current) return undefined;
    const holders = containerRef.current.querySelectorAll("[data-page]");

    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) renderPage(Number(e.target.dataset.page), e.target);
        }
      },
      { rootMargin: "1200px 0px" }
    );
    holders.forEach((h) => io.observe(h));

    // Jump to the saved page
    requestAnimationFrame(() => {
      const target = containerRef.current?.querySelector(`[data-page="${restoreTo.current}"]`);
      if (target && restoreTo.current > 1) {
        target.scrollIntoView();
      }
    });

    let raf = 0;
    const onScroll = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        const anchor = window.scrollY + window.innerHeight * 0.4;
        let page = 1;
        for (const h of holders) {
          if (h.offsetTop <= anchor) page = Number(h.dataset.page);
        }
        setCurrent(page);
        clearTimeout(saveTimer.current);
        saveTimer.current = setTimeout(() => persist(page), 1200);
      });
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      io.disconnect();
      window.removeEventListener("scroll", onScroll);
      cancelAnimationFrame(raf);
      clearTimeout(saveTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doc, numPages]);

  if (error) {
    return (
      <div className="reader reader--paper">
        <div className="reader__error">
          <p>{t("reader.pdfFail")}</p>
          <Link to={`/book/${bookId}`} className="btn btn-ghost">{t("reader.back")}</Link>
        </div>
      </div>
    );
  }

  const ratio = pageSize ? pageSize.height / pageSize.width : 1.4;
  const overall = numPages > 0 ? current / numPages : 0;

  return (
    <div className="reader reader--paper pdf-reader">
      <header className="reader__bar">
        <Link to={`/book/${bookId}`} className="reader__bar-btn" title={t("reader.toBook")}>←</Link>
        <div className="reader__bar-title">
          <span className="reader__bar-book">{meta.title}</span>
          {meta.authors && <span className="reader__bar-author">{meta.authors}</span>}
        </div>
        <a className="reader__bar-btn" href={meta.fileUrl} title={t("reader.openExternal")} target="_blank" rel="noreferrer">⤓</a>
      </header>

      {!doc && <div className="reader__loading">{t("reader.pdfLoading")}</div>}

      <main className="pdf-reader__pages" ref={containerRef}>
        {doc &&
          Array.from({ length: numPages }, (_, i) => (
            <div
              key={i + 1}
              data-page={i + 1}
              className="pdf-reader__page"
              style={{ aspectRatio: `${1 / ratio}` }}
            />
          ))}
      </main>

      <footer className="reader__progress" aria-label="progress">
        <div className="reader__progress-fill" style={{ width: `${Math.round(overall * 1000) / 10}%` }} />
        <div className="reader__progress-info">
          <span className="reader__progress-chapter">{t("reader.page", { n: current, total: numPages || "…" })}</span>
          <span className="reader__progress-pct">{Math.round(overall * 100)}%</span>
        </div>
      </footer>
    </div>
  );
};

export default PdfReader;
