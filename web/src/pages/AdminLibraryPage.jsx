import { t } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { fetchSettings, importInpx, importStatus, saveSettings, uploadBooks } from "../api/manage";
import "./AdminLibraryPage.css";

const ENRICH_SOURCES = () => [
  { id: "livelib", label: "LiveLib", hint: t("admin.livelib.hint") },
  { id: "google_books", label: "Google Books", hint: t("admin.google.hint") },
  { id: "open_library", label: "Open Library", hint: t("admin.openlibrary.hint") },
];

const PHASE_LABELS = () => ({
  starting: t("admin.import.starting"),
  clearing: t("admin.import.clearing"),
  loading: t("admin.import.loading"),
  indexing: t("admin.import.indexing"),
});

const AdminLibraryPage = () => {
  // --- Book upload ---
  const [uploading, setUploading] = useState(false);
  const [results, setResults] = useState([]);
  const fileInput = useRef(null);
  const pendingFiles = useRef(new Map()); // file name -> File, for re-uploading with force

  const mergeResults = (incoming) =>
    setResults((prev) => [
      ...incoming,
      ...prev.filter((p) => !incoming.some((n) => n.name === p.name)),
    ]);

  const onUpload = (e) => {
    const files = Array.from(e.target.files ?? []);
    if (!files.length) return;
    setUploading(true);
    for (const f of files) pendingFiles.current.set(f.name, f);
    uploadBooks(files)
      .then((res) => mergeResults(res.results ?? []))
      .catch(() => alert(t("admin.upload.fail")))
      .finally(() => {
        setUploading(false);
        if (fileInput.current) fileInput.current.value = "";
      });
  };

  const forceUpload = (name) => {
    const file = pendingFiles.current.get(name);
    if (!file || uploading) return;
    setUploading(true);
    uploadBooks([file], { force: true })
      .then((res) => mergeResults(res.results ?? []))
      .catch(() => alert(t("admin.upload.fail")))
      .finally(() => setUploading(false));
  };

  // --- inpx import ---
  const [inpxFile, setInpxFile] = useState(null);
  const [replace, setReplace] = useState(false);
  const [status, setStatus] = useState(null);
  const polling = useRef(null);

  const refreshStatus = () =>
    importStatus()
      .then((st) => {
        setStatus(st);
        if (!st.running && polling.current) {
          clearInterval(polling.current);
          polling.current = null;
        }
      })
      .catch(() => {});

  useEffect(() => {
    refreshStatus();
    return () => polling.current && clearInterval(polling.current);
  }, []);

  const startImport = () => {
    if (!inpxFile) return;
    if (replace && !confirm(t("admin.import.confirm"))) return;
    importInpx(inpxFile, replace)
      .then(() => {
        setInpxFile(null);
        refreshStatus();
        polling.current = setInterval(refreshStatus, 1000);
      })
      .catch((err) => {
        alert(err.status === 409 ? t("admin.import.conflict") : t("admin.import.fail"));
      });
  };

  const importRunning = Boolean(status?.running);

  // --- Sources for external ratings and similar books ---
  const [enrichment, setEnrichment] = useState(null);
  const [similar, setSimilar] = useState(null);
  const [tastediveKey, setTastediveKey] = useState("");

  useEffect(() => {
    fetchSettings()
      .then((res) => {
        setEnrichment(res.enrichment ?? null);
        setSimilar(res.similar ?? null);
        setTastediveKey(res.tastediveKey ?? "");
      })
      .catch(() => setEnrichment(null));
  }, []);

  const toggleSource = (id) => {
    const next = { ...enrichment, [id]: !enrichment[id] };
    setEnrichment(next);
    saveSettings({ enrichment: next }).catch(() => {
      setEnrichment(enrichment); // roll back on error
      alert(t("admin.sources.saveFail"));
    });
  };

  const toggleSimilar = (id) => {
    const next = { ...similar, [id]: !similar[id] };
    setSimilar(next);
    saveSettings({ similar: next }).catch(() => {
      setSimilar(similar);
      alert(t("admin.sources.saveFail"));
    });
  };

  const saveTastediveKey = () => {
    saveSettings({ tastediveKey })
      .then((res) => {
        setSimilar(res.similar ?? similar);
        alert(t("admin.tastedive.saved"));
      })
      .catch(() => alert(t("admin.tastedive.saveFail")));
  };

  return (
    <div className="container library-admin">
      <header className="library-admin__header">
        <h1>{t("admin.title")}</h1>
      </header>

      <section className="library-admin__section">
        <h2>{t("admin.upload")}</h2>
        <p className="library-admin__hint">
          {t("admin.upload.hint")}
        </p>
        <label className={`library-admin__dropzone ${uploading ? "is-busy" : ""}`}>
          <input
            ref={fileInput}
            type="file"
            multiple
            accept=".fb2,.epub,.pdf,.djvu,.txt,.mobi,.azw3"
            onChange={onUpload}
            disabled={uploading}
          />
          {uploading ? t("admin.upload.busy") : t("admin.upload.drop")}
        </label>

        {results.length > 0 && (
          <ul className="library-admin__results">
            {results.map((r, i) => (
              <li key={`${r.name}-${i}`} className={r.error ? "is-error" : ""}>
                {r.error ? (
                  <>
                    <span className="library-admin__file">{r.name}</span>
                    <span className="library-admin__error">
                      {r.error}
                      {r.duplicate && (
                        <>
                          {": "}
                          <Link to={`/book/${r.duplicate.bookId}`}>«{r.duplicate.title}»</Link>
                        </>
                      )}
                    </span>
                  </>
                ) : r.needConfirm ? (
                  <>
                    <span className="library-admin__file">{r.name}</span>
                    <span className="library-admin__warn">
                      {t("admin.upload.similar")}{" "}
                      <Link to={`/book/${r.duplicate.bookId}`}>
                        «{r.duplicate.title}»{r.duplicate.authors ? ` — ${r.duplicate.authors}` : ""}
                      </Link>
                    </span>
                    <button
                      type="button"
                      className="btn btn-link"
                      disabled={uploading}
                      onClick={() => forceUpload(r.name)}
                    >
                      {t("admin.upload.force")}
                    </button>
                  </>
                ) : (
                  <>
                    <Link to={`/book/${r.bookId}`} className="library-admin__file">
                      {r.title}
                    </Link>
                    {r.authors && <span className="library-admin__authors">{r.authors}</span>}
                    <span className="library-admin__ok">{t("admin.upload.added")}</span>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="library-admin__section">
        <h2>{t("admin.import")}</h2>
        <p className="library-admin__hint">
          {t("admin.import.hint")}
        </p>

        <div className="library-admin__import-row">
          <input
            type="file"
            accept=".inpx"
            onChange={(e) => setInpxFile(e.target.files?.[0] ?? null)}
            disabled={importRunning}
          />
          <label className="library-admin__replace">
            <input
              type="checkbox"
              checked={replace}
              onChange={(e) => setReplace(e.target.checked)}
              disabled={importRunning}
            />
            {t("admin.import.replace")}
          </label>
          <button
            type="button"
            className="btn btn-primary"
            onClick={startImport}
            disabled={!inpxFile || importRunning}
          >
            {t("admin.import.go")}
          </button>
        </div>

        {status && (status.running || status.phase === "done" || status.phase === "error") && (
          <div className={`library-admin__status library-admin__status--${status.phase}`}>
            {status.running && (
              <>
                <span className="library-admin__spinner" aria-hidden="true" />
                {PHASE_LABELS()[status.phase] ?? status.phase}
                {status.phase === "loading" && status.processed > 0 && (
                  <>{t("admin.import.processed", { n: status.processed.toLocaleString() })}</>
                )}
              </>
            )}
            {!status.running && status.phase === "done" && (
              <>
                {t("admin.import.done", {
                  books: Number(status.books).toLocaleString(),
                  authors: Number(status.authors).toLocaleString(),
                  series: Number(status.series).toLocaleString(),
                })}
              </>
            )}
            {!status.running && status.phase === "error" && <>{t("admin.import.error", { err: status.error })}</>}
          </div>
        )}
      </section>

      <section className="library-admin__section">
        <h2>{t("admin.sources")}</h2>
        <p className="library-admin__hint">
          {t("admin.sources.hint")}
        </p>
        {!enrichment && <div className="library-admin__hint">{t("admin.sources.loading")}</div>}
        {enrichment && (
          <ul className="library-admin__sources">
            {ENRICH_SOURCES().map((src) => (
              <li key={src.id}>
                <label className="library-admin__source">
                  <input
                    type="checkbox"
                    checked={Boolean(enrichment[src.id])}
                    onChange={() => toggleSource(src.id)}
                  />
                  <span className="library-admin__source-name">{src.label}</span>
                  <span className="library-admin__source-hint">{src.hint}</span>
                </label>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="library-admin__section">
        <h2>{t("admin.similar")}</h2>
        <p className="library-admin__hint">
          {t("admin.similar.hint")}
        </p>
        {similar && (
          <ul className="library-admin__sources">
            <li>
              <label className="library-admin__source">
                <input type="checkbox" checked={Boolean(similar.fantlab)} onChange={() => toggleSimilar("fantlab")} />
                <span className="library-admin__source-name">{t("source.fantlab")}</span>
                <span className="library-admin__source-hint">{t("admin.fantlab.hint")}</span>
              </label>
            </li>
            <li>
              <label className="library-admin__source">
                <input type="checkbox" checked={Boolean(similar.tastedive)} onChange={() => toggleSimilar("tastedive")} />
                <span className="library-admin__source-name">TasteDive</span>
                <span className="library-admin__source-hint">{t("admin.tastedive.hint")}</span>
              </label>
            </li>
          </ul>
        )}
        <div className="library-admin__key-row">
          <input
            type="text"
            placeholder={t("admin.tastedive.key")}
            value={tastediveKey}
            onChange={(e) => setTastediveKey(e.target.value)}
          />
          <button type="button" className="btn btn-primary" onClick={saveTastediveKey}>
            {t("admin.tastedive.save")}
          </button>
        </div>
      </section>
    </div>
  );
};

export default AdminLibraryPage;
