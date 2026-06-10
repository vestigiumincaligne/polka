import { t } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { addToList, createList, fetchLists, removeFromList } from "../api/lists";
import "./ListsMenu.css";

// "Add to list" dropdown: toggle the book in any list,
// or create a new list right from the menu.
const ListsMenu = ({ bookId, memberIds, onChange }) => {
  const [open, setOpen] = useState(false);
  const [lists, setLists] = useState(null);
  const [newName, setNewName] = useState("");
  const [busy, setBusy] = useState(false);
  const rootRef = useRef(null);

  useEffect(() => {
    if (!open || lists) return;
    fetchLists()
      .then((res) => setLists(res.lists ?? []))
      .catch(() => setLists([]));
  }, [open, lists]);

  useEffect(() => {
    if (!open) return undefined;
    const onDocClick = (e) => {
      if (!rootRef.current?.contains(e.target)) setOpen(false);
    };
    document.addEventListener("mousedown", onDocClick);
    return () => document.removeEventListener("mousedown", onDocClick);
  }, [open]);

  const member = new Set(memberIds ?? []);

  const toggle = (list) => {
    if (busy) return;
    setBusy(true);
    const inList = member.has(list.id);
    (inList ? removeFromList(list.id, bookId) : addToList(list.id, bookId))
      .then(() => onChange(list.id, !inList))
      .catch(() => {})
      .finally(() => setBusy(false));
  };

  const submitNew = (e) => {
    e.preventDefault();
    const name = newName.trim();
    if (!name || busy) return;
    setBusy(true);
    createList(name)
      .then((res) =>
        addToList(res.list.id, bookId).then(() => {
          setLists((prev) => [...(prev ?? []), { ...res.list, books: 1 }]);
          setNewName("");
          onChange(res.list.id, true);
        })
      )
      .catch((err) => alert(err.status === 409 ? t("lists.nameTaken") : t("lists.createFail")))
      .finally(() => setBusy(false));
  };

  return (
    <div className="lists-menu" ref={rootRef}>
      <button type="button" className="btn btn-ghost" onClick={() => setOpen((v) => !v)}>
        {t("lists.inList")} {member.size > 0 ? `(${member.size})` : "+"}
      </button>

      {open && (
        <div className="lists-menu__pop">
          {!lists && <div className="lists-menu__hint">{t("loading")}</div>}
          {lists?.map((l) => (
            <button
              key={l.id}
              type="button"
              className="lists-menu__item"
              onClick={() => toggle(l)}
              disabled={busy}
            >
              <span className={`lists-menu__check ${member.has(l.id) ? "is-on" : ""}`}>✓</span>
              {l.name}
            </button>
          ))}
          {lists?.length === 0 && <div className="lists-menu__hint">{t("lists.none")}</div>}
          <form className="lists-menu__new" onSubmit={submitNew}>
            <input
              type="text"
              placeholder={t("lists.new")}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <button type="submit" className="btn btn-primary" disabled={!newName.trim() || busy}>
              +
            </button>
          </form>
        </div>
      )}
    </div>
  );
};

export default ListsMenu;
