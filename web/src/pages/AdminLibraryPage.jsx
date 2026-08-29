import { t } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { fetchSettings, importInpx, importStatus, saveSettings, testSmtp, uploadBooks } from "../api/manage";
import { deleteCollection, fetchCollections, importCollection, rematchCollection, syncCollectionSource } from "../api/collections";
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
  const [inpxPath, setInpxPath] = useState("");
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
    if (!inpxFile && !inpxPath.trim()) return;
    if (replace && !confirm(t("admin.import.confirm"))) return;
    importInpx({ file: inpxFile, path: inpxPath.trim(), replace })
      .then(() => {
        setInpxFile(null);
        setInpxPath("");
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
  const [opdsEnabled, setOpdsEnabled] = useState(true);
  const [opdsCopied, setOpdsCopied] = useState(false);
  const [smtp, setSmtp] = useState(null);
  const [smtpPwd, setSmtpPwd] = useState("");
  const [smtpMsg, setSmtpMsg] = useState(null);
  const [library, setLibrary] = useState(null);
  const [colSources, setColSources] = useState(null);

  useEffect(() => {
    fetchSettings()
      .then((res) => {
        setEnrichment(res.enrichment ?? null);
        setColSources(res.collectionSources ?? []);
        setSimilar(res.similar ?? null);
        setTastediveKey(res.tastediveKey ?? "");
        setOpdsEnabled(res.opdsEnabled !== false);
        setSmtp(res.smtp ?? { security: "starttls", port: 587 });
        setLibrary(res.library ?? null);
      })
      .catch(() => setEnrichment(null));
  }, []);

  // An empty / missing library root almost always means the mounted path and
  // POLKA_LIBRARY_DIR disagree: the catalog imports, but books "won't open"
  // and covers are missing.
  const libraryEmpty = library && library.configured && (!library.exists || library.entries === 0);

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

  const opdsUrl = `${window.location.origin}/opds`;

  const toggleOpds = () => {
    const next = !opdsEnabled;
    setOpdsEnabled(next);
    saveSettings({ opdsEnabled: next }).catch(() => setOpdsEnabled(!next));
  };

  const copyOpds = () => {
    navigator.clipboard?.writeText(opdsUrl).then(() => {
      setOpdsCopied(true);
      setTimeout(() => setOpdsCopied(false), 1500);
    });
  };

  const smtpField = (key, value) => setSmtp((prev) => ({ ...prev, [key]: value }));

  const saveSmtp = () => {
    const payload = { ...smtp, port: Number(smtp.port) || 0 };
    if (smtpPwd) payload.password = smtpPwd;
    setSmtpMsg(null);
    saveSettings({ smtp: payload })
      .then((res) => {
        setSmtp(res.smtp ?? smtp);
        setSmtpPwd("");
        setSmtpMsg({ ok: true, text: t("admin.smtp.saved") });
      })
      .catch(() => setSmtpMsg({ ok: false, text: t("admin.smtp.saveFail") }));
  };

  const checkSmtp = () => {
    setSmtpMsg({ ok: true, text: t("admin.smtp.checking") });
    testSmtp()
      .then((res) =>
        setSmtpMsg(res.ok ? { ok: true, text: t("admin.smtp.ok") } : { ok: false, text: res.error || t("admin.smtp.checkFail") })
      )
      .catch(() => setSmtpMsg({ ok: false, text: t("admin.smtp.checkFail") }));
  };

  // --- подборки ---
  const [collections, setCollections] = useState([]);
  const [colBusy, setColBusy] = useState(false);
  const [colMsg, setColMsg] = useState(null);
  const colInputRef = useRef(null);
  const reloadCollections = () =>
    fetchCollections()
      .then((res) => setCollections(res.collections ?? []))
      .catch(() => setCollections([]));
  useEffect(() => {
    reloadCollections();
  }, []);
  const onCollectionFiles = async (files) => {
    if (!files?.length) return;
    setColBusy(true);
    setColMsg(null);
    try {
      for (const f of files) {
        const c = await importCollection(f);
        setColMsg({ ok: true, text: t("admin.collections.imported", { title: c.title, matched: c.matched, total: c.total }) });
      }
      await reloadCollections();
    } catch (err) {
      setColMsg({ ok: false, text: t("admin.collections.fail", { err: err.message || "" }) });
    } finally {
      setColBusy(false);
      if (colInputRef.current) colInputRef.current.value = "";
    }
  };
  const rematch = (slug) => {
    setColBusy(true);
    rematchCollection(slug)
      .then((c) => {
        setColMsg({ ok: true, text: t("admin.collections.imported", { title: c.title, matched: c.matched, total: c.total }) });
        return reloadCollections();
      })
      .catch(() => setColMsg({ ok: false, text: t("admin.collections.fail", { err: "" }) }))
      .finally(() => setColBusy(false));
  };
  const SOURCE_LABELS = { forbes: { label: "Forbes.ru", hint: t("admin.collections.forbes.hint") } };
  const toggleColSource = (id, on) => {
    saveSettings({ collectionSources: { [id]: on } })
      .then((res) => {
        setColSources(res.collectionSources ?? []);
        if (on) setColMsg({ ok: true, text: t("admin.collections.source.started") });
      })
      .catch(() => alert(t("admin.sources.saveFail")));
  };
  const syncColSource = (id) => {
    syncCollectionSource(id)
      .then(() => setColMsg({ ok: true, text: t("admin.collections.source.started") }))
      .catch(() => setColMsg({ ok: false, text: t("admin.collections.fail", { err: "" }) }));
    setTimeout(() => fetchSettings().then((res) => setColSources(res.collectionSources ?? [])).then(reloadCollections), 8000);
  };
  const removeCollection = (c) => {
    if (!confirm(t("admin.collections.confirmDelete", { title: c.title }))) return;
    deleteCollection(c.slug)
      .then(reloadCollections)
      .catch(() => setColMsg({ ok: false, text: t("admin.collections.fail", { err: "" }) }));
  };

  return (
    <div className="container library-admin">
      <header className="library-admin__header">
        <h1>{t("admin.title")}</h1>
      </header>

      {libraryEmpty && (
        <div className="library-admin__banner library-admin__banner--warning">
          {t("admin.library.empty", { dir: library.dir })}
        </div>
      )}

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
        <h2>{t("admin.collections")}</h2>
        <p className="library-admin__hint">
          {t("admin.collections.hint")}{" "}
          <a href="https://github.com/vestigiumincaligne/polka/tree/main/collections" target="_blank" rel="noreferrer">
            {t("admin.collections.examples")}
          </a>
          {" · "}
          {t("admin.collections.bundled")}
        </p>
        <div className="library-admin__import-row">
          <input
            ref={colInputRef}
            type="file"
            accept=".json,application/json"
            multiple
            disabled={colBusy}
            onChange={(e) => onCollectionFiles(Array.from(e.target.files ?? []))}
          />
          {colBusy && <span className="library-admin__spinner" aria-hidden="true" />}
          {colMsg && (
            <span className={colMsg.ok ? "library-admin__ok" : "library-admin__error"}>{colMsg.text}</span>
          )}
        </div>
        {colSources && colSources.length > 0 && (
          <>
            <h3 className="library-admin__subtitle">{t("admin.collections.sources")}</h3>
            <ul className="library-admin__sources">
              {colSources.map((src) => {
                const meta = SOURCE_LABELS[src.id] ?? { label: src.id, hint: "" };
                return (
                  <li key={src.id}>
                    <label className="library-admin__source">
                      <input
                        type="checkbox"
                        checked={Boolean(src.enabled)}
                        onChange={(e) => toggleColSource(src.id, e.target.checked)}
                      />
                      <span className="library-admin__source-name">{meta.label}</span>
                      <span className="library-admin__source-hint">
                        {meta.hint}
                        {src.last && (
                          <>
                            {" "}
                            {src.last.error
                              ? t("admin.collections.source.error", { err: src.last.error })
                              : t("admin.collections.source.last", { n: src.last.added, at: new Date(src.last.at).toLocaleString() })}
                          </>
                        )}
                      </span>
                      {src.enabled && (
                        <button type="button" className="btn btn-ghost" disabled={src.running} onClick={() => syncColSource(src.id)}>
                          {src.running ? t("admin.collections.source.running") : t("admin.collections.source.sync")}
                        </button>
                      )}
                    </label>
                  </li>
                );
              })}
            </ul>
          </>
        )}
        {collections.length > 0 && (
          <ul className="library-admin__results library-admin__collections">
            {collections.map((c) => (
              <li key={c.slug}>
                <Link to={`/shelf/${encodeURIComponent(c.shelfId)}`} className="library-admin__file">
                  {c.title}
                </Link>
                <span className="library-admin__authors">
                  {" "}
                  {t("collection.count", { matched: c.matched, total: c.total })}
                  {c.source ? ` · ${c.source}` : ""}
                  {c.origin && c.origin !== "bundled" && colSources?.some((s) => s.id === c.origin && !s.enabled)
                    ? ` · ${t("admin.collections.hiddenSource")}`
                    : ""}
                </span>
                <span className="library-admin__collection-actions">
                  <button type="button" className="btn btn-ghost" disabled={colBusy} onClick={() => rematch(c.slug)}>
                    {t("admin.collections.rematch")}
                  </button>
                  <button type="button" className="btn btn-ghost" disabled={colBusy} onClick={() => removeCollection(c)}>
                    {t("admin.collections.delete")}
                  </button>
                </span>
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
            disabled={(!inpxFile && !inpxPath.trim()) || importRunning}
          >
            {t("admin.import.go")}
          </button>
        </div>

        <div className="library-admin__import-path">
          <span className="library-admin__import-or">{t("admin.import.or")}</span>
          <input
            type="text"
            placeholder={t("admin.import.pathPlaceholder")}
            value={inpxPath}
            onChange={(e) => setInpxPath(e.target.value)}
            disabled={importRunning}
          />
          <p className="library-admin__hint">{t("admin.import.pathHint")}</p>
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

        {status && !status.running && status.warning && (
          <div className="library-admin__status library-admin__status--warning">
            ⚠ {status.warning}
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

      <section className="library-admin__section">
        <h2>{t("admin.opds")}</h2>
        <p className="library-admin__hint">{t("admin.opds.hint")}</p>
        <label className="library-admin__source library-admin__opds-toggle">
          <input type="checkbox" checked={opdsEnabled} onChange={toggleOpds} />
          <span className="library-admin__source-name">{t("admin.opds.toggle")}</span>
        </label>
        {opdsEnabled ? (
          <div className="library-admin__opds-url">
            <span className="library-admin__opds-label">{t("admin.opds.url")}</span>
            <code>{opdsUrl}</code>
            <button type="button" className="btn btn-ghost" onClick={copyOpds}>
              {opdsCopied ? t("admin.opds.copied") : t("admin.opds.copy")}
            </button>
          </div>
        ) : (
          <p className="library-admin__hint">{t("admin.opds.off")}</p>
        )}
      </section>

      <section className="library-admin__section">
        <h2>{t("admin.smtp")}</h2>
        <p className="library-admin__hint">{t("admin.smtp.hint")}</p>
        {smtp && (
          <>
            <label className="library-admin__source library-admin__opds-toggle">
              <input type="checkbox" checked={Boolean(smtp.enabled)} onChange={(e) => smtpField("enabled", e.target.checked)} />
              <span className="library-admin__source-name">{t("admin.smtp.enable")}</span>
            </label>
            <div className="library-admin__smtp-grid">
              <input placeholder={t("admin.smtp.host")} value={smtp.host || ""} onChange={(e) => smtpField("host", e.target.value)} />
              <input placeholder={t("admin.smtp.port")} value={smtp.port || ""} onChange={(e) => smtpField("port", e.target.value)} style={{ maxWidth: 110 }} />
              <select value={smtp.security || "starttls"} onChange={(e) => smtpField("security", e.target.value)}>
                <option value="starttls">STARTTLS (587)</option>
                <option value="tls">SSL/TLS (465)</option>
                <option value="none">{t("admin.smtp.none")}</option>
              </select>
              <input placeholder={t("admin.smtp.user")} value={smtp.user || ""} onChange={(e) => smtpField("user", e.target.value)} />
              <input
                type="password"
                placeholder={smtp.hasPassword ? t("admin.smtp.pwdSet") : t("admin.smtp.pwd")}
                value={smtpPwd}
                onChange={(e) => setSmtpPwd(e.target.value)}
              />
              <input placeholder={t("admin.smtp.from")} value={smtp.from || ""} onChange={(e) => smtpField("from", e.target.value)} />
            </div>
            <div className="library-admin__key-row">
              <button type="button" className="btn btn-primary" onClick={saveSmtp}>{t("admin.smtp.save")}</button>
              <button type="button" className="btn btn-ghost" onClick={checkSmtp}>{t("admin.smtp.check")}</button>
              {smtpMsg && (
                <span className={smtpMsg.ok ? "library-admin__ok" : "library-admin__error"}>{smtpMsg.text}</span>
              )}
            </div>
          </>
        )}
      </section>
    </div>
  );
};

export default AdminLibraryPage;
