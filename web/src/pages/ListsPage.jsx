import { t } from "../i18n";
import { useCallback, useEffect, useState } from "react";
import BookCard from "../components/BookCard";
import {
  createList,
  deleteList,
  fetchListBooks,
  fetchLists,
  removeFromList,
  renameList,
} from "../api/lists";
import "./ListsPage.css";

const ListsPage = () => {
  const [lists, setLists] = useState(null);
  const [activeId, setActiveId] = useState(null);
  const [books, setBooks] = useState([]);
  const [hasMore, setHasMore] = useState(false);
  const [nextOffset, setNextOffset] = useState(0);
  const [loading, setLoading] = useState(false);
  const [newName, setNewName] = useState("");

  const reloadLists = useCallback(
    (keepActive = true) =>
      fetchLists().then((res) => {
        const ls = res.lists ?? [];
        setLists(ls);
        if (!keepActive || !ls.some((l) => l.id === activeId)) {
          setActiveId(ls[0]?.id ?? null);
        }
        return ls;
      }),
    [activeId]
  );

  useEffect(() => {
    reloadLists(false).catch(() => setLists([]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!activeId) {
      setBooks([]);
      return undefined;
    }
    let cancelled = false;
    setLoading(true);
    fetchListBooks(activeId)
      .then((res) => {
        if (cancelled) return;
        setBooks(res.titlesList ?? []);
        setHasMore(Boolean(res.hasMore));
        setNextOffset(Number(res.nextOffset ?? 0));
      })
      .catch(() => !cancelled && setBooks([]))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [activeId]);

  const loadMore = () => {
    fetchListBooks(activeId, { offset: nextOffset }).then((res) => {
      setBooks((prev) => [...prev, ...(res.titlesList ?? [])]);
      setHasMore(Boolean(res.hasMore));
      setNextOffset(Number(res.nextOffset ?? nextOffset));
    });
  };

  const active = lists?.find((l) => l.id === activeId);

  const submitNew = (e) => {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    createList(name)
      .then((res) => {
        setNewName("");
        reloadLists().then(() => setActiveId(res.list.id));
      })
      .catch((err) => alert(err.status === 409 ? t("lists.nameTaken") : t("lists.createFail")));
  };

  const rename = () => {
    const name = prompt(t("lists.renamePrompt"), active?.name);
    if (!name || name === active?.name) return;
    renameList(activeId, name)
      .then(() => reloadLists())
      .catch((err) => alert(err.status === 409 ? t("lists.nameTaken") : t("lists.renameFail")));
  };

  const remove = () => {
    if (!confirm(t("lists.deleteConfirm", { name: active?.name }))) return;
    deleteList(activeId)
      .then(() => reloadLists(false))
      .catch(() => alert(t("lists.deleteFail")));
  };

  const removeBook = (bookId) => {
    removeFromList(activeId, bookId).then(() => {
      setBooks((prev) => prev.filter((b) => b.BookID !== bookId));
      reloadLists();
    });
  };

  return (
    <div className="container lists-page">
      <header className="lists-page__header">
        <h1>{t("lists.title")}</h1>
      </header>

      {lists === null && <div className="lists-page__hint">{t("loading")}</div>}

      {lists !== null && (
        <>
          <div className="lists-page__tabs">
            {lists.map((l) => (
              <button
                key={l.id}
                type="button"
                className={`lists-page__tab ${l.id === activeId ? "is-active" : ""}`}
                onClick={() => setActiveId(l.id)}
              >
                {l.name}
                <span className="lists-page__tab-count">{l.books}</span>
              </button>
            ))}
            <form className="lists-page__new" onSubmit={submitNew}>
              <input
                type="text"
                placeholder={t("lists.new")}
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
              />
              <button type="submit" className="btn btn-primary" disabled={!newName.trim()}>
                {t("lists.create")}
              </button>
            </form>
          </div>

          {active && (
            <div className="lists-page__actions">
              {!active.builtin && (
                <>
                  <button type="button" className="btn btn-link" onClick={rename}>
                    {t("lists.rename")}
                  </button>
                  <button type="button" className="btn btn-link lists-page__danger" onClick={remove}>
                    {t("lists.delete")}
                  </button>
                </>
              )}
            </div>
          )}

          {loading && <div className="lists-page__hint">{t("loading")}</div>}

          {!loading && active && books.length === 0 && (
            <div className="lists-page__hint">
              {t("lists.empty")}
            </div>
          )}

          {!loading && books.length > 0 && (
            <>
              <div className="lists-page__grid">
                {books.map((b) => (
                  <div className="lists-page__cell" key={b.BookID}>
                    <BookCard book={b} />
                    <button
                      type="button"
                      className="btn btn-link lists-page__remove"
                      onClick={() => removeBook(b.BookID)}
                    >
                      {t("lists.remove")}
                    </button>
                  </div>
                ))}
              </div>
              {hasMore && (
                <div className="lists-page__more">
                  <button type="button" className="btn btn-primary" onClick={loadMore}>
                    {t("lists.more")}
                  </button>
                </div>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
};

export default ListsPage;
