import { t } from "../i18n";
import { useEffect, useState } from "react";
import BookCard from "../components/BookCard";
import {
  disconnectSync,
  fetchOfflineBooks,
  fetchSyncConfig,
  fetchSyncInfo,
  removeOffline,
  saveSyncConfig,
  syncNow,
} from "../api/sync";
import "./SyncPage.css";

// "Server" page: connection to a Polka server, sync status
// and the client's offline books.
const SyncPage = () => {
  const [info, setInfo] = useState(null);
  const [books, setBooks] = useState([]);
  const [busy, setBusy] = useState(false);
  const [restartNeeded, setRestartNeeded] = useState(false);

  // Connection form
  const [form, setForm] = useState({ server: "", login: "", password: "" });
  const [formError, setFormError] = useState(null);
  const [saving, setSaving] = useState(false);

  const reload = () => {
    fetchSyncInfo().then(setInfo).catch(() => setInfo(null));
    fetchSyncConfig()
      .then((cfg) =>
        setForm((prev) => ({
          ...prev,
          server: cfg.server ?? prev.server,
          login: cfg.login ?? prev.login,
        }))
      )
      .catch(() => {});
    fetchOfflineBooks()
      .then((res) => setBooks(res.books ?? []))
      .catch(() => setBooks([]));
  };

  useEffect(reload, []);

  const enabled = Boolean(info?.enabled);

  const submit = (e) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setFormError(null);
    saveSyncConfig(form)
      .then(() => {
        setRestartNeeded(true);
        setForm((prev) => ({ ...prev, password: "" }));
      })
      .catch((err) => setFormError(err.message || t("sync.form.fail")))
      .finally(() => setSaving(false));
  };

  const disconnect = () => {
    if (!confirm(t("sync.disconnectConfirm"))) return;
    disconnectSync()
      .then(() => setRestartNeeded(true))
      .catch(() => alert(t("sync.disconnectFail")));
  };

  const doSync = () => {
    setBusy(true);
    syncNow()
      .then(setInfo)
      .catch(() => alert(t("sync.nowFail")))
      .finally(() => setBusy(false));
  };

  const remove = (id) => {
    removeOffline(id).then(() => setBooks((prev) => prev.filter((b) => b.BookID !== id)));
  };

  const lastSync = info?.lastSync ? new Date(info.lastSync).toLocaleString() : t("sync.never");

  return (
    <div className="container sync-page">
      <header className="sync-page__header">
        <h1>{t("sync.title")}</h1>
        {!enabled && (
          <p className="sync-page__hint">
            {t("sync.intro")}
          </p>
        )}
      </header>

      {restartNeeded && (
        <div className="sync-page__restart">
          <span dangerouslySetInnerHTML={{ __html: t("sync.restart") }} />
        </div>
      )}

      {enabled && info && (
        <div className="sync-page__status">
          <div className="sync-page__row">
            <span className="sync-page__label">{t("sync.addr")}</span>
            <span>{info.server}</span>
          </div>
          <div className="sync-page__row">
            <span className="sync-page__label">{t("sync.state")}</span>
            <span className={info.online ? "sync-page__online" : "sync-page__offline"}>
              {info.online ? t("sync.online") : t("sync.offline")}
            </span>
          </div>
          <div className="sync-page__row">
            <span className="sync-page__label">{t("sync.last")}</span>
            <span>{lastSync}</span>
          </div>
          <div className="sync-page__actions-row">
            <button type="button" className="btn btn-primary" onClick={doSync} disabled={busy}>
              {busy ? t("sync.nowBusy") : t("sync.now")}
            </button>
            <button type="button" className="btn btn-link sync-page__danger" onClick={disconnect}>
              {t("sync.disconnect")}
            </button>
          </div>
          <p className="sync-page__hint">
            {t("sync.auto")}
          </p>
        </div>
      )}

      <form className="sync-page__form" onSubmit={submit}>
        <h2>{enabled ? t("sync.form.change") : t("sync.form.new")}</h2>
        <label className="sync-page__field">
          <span>{t("sync.form.server")}</span>
          <input
            type="url"
            placeholder="http://192.168.1.10:12791"
            value={form.server}
            onChange={(e) => setForm({ ...form, server: e.target.value })}
            required
          />
        </label>
        <label className="sync-page__field">
          <span>{t("login.login")}</span>
          <input
            type="text"
            autoComplete="username"
            value={form.login}
            onChange={(e) => setForm({ ...form, login: e.target.value })}
            required
          />
        </label>
        <label className="sync-page__field">
          <span>{t("login.password")}</span>
          <input
            type="password"
            autoComplete="current-password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
            required
          />
        </label>
        {formError && <div className="sync-page__error">{formError}</div>}
        <button type="submit" className="btn btn-primary" disabled={saving}>
          {saving ? t("sync.form.checking") : t("sync.form.check")}
        </button>
      </form>

      {enabled && (
        <section className="sync-page__offline-books">
          <h2>{t("sync.offlineBooks")}</h2>
          {books.length === 0 && (
            <p className="sync-page__hint">
              {t("sync.offlineEmpty")}
            </p>
          )}
          {books.length > 0 && (
            <div className="sync-page__grid">
              {books.map((b) => (
                <div className="sync-page__cell" key={b.BookID}>
                  <BookCard book={b} />
                  <button type="button" className="btn btn-link sync-page__remove" onClick={() => remove(b.BookID)}>
                    {t("sync.removeDevice")}
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  );
};

export default SyncPage;
