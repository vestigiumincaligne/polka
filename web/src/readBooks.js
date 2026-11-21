// The set of books the signed-in user marked read — for the ✓ badge on book
// cards. Loaded once per session, kept in sync by the "Read" toggle.
import { useSyncExternalStore } from "react";
import { fetchReadIds } from "./api/lists";

let ids = new Set();
let loading = null;
const listeners = new Set();

const emit = () => {
  ids = new Set(ids); // new identity so subscribers re-render
  listeners.forEach((l) => l());
};

export const loadReadBooks = () => {
  if (!loading) {
    loading = fetchReadIds()
      .then((res) => {
        ids = new Set((res?.ids ?? []).map(Number));
        listeners.forEach((l) => l());
      })
      .catch(() => {
        loading = null; // anonymous or offline: try again next time
      });
  }
  return loading;
};

export const resetReadBooks = () => {
  ids = new Set();
  loading = null;
  listeners.forEach((l) => l());
};

export const setBookRead = (bookId, read) => {
  const id = Number(bookId);
  if (read === ids.has(id)) return;
  if (read) ids.add(id);
  else ids.delete(id);
  emit();
};

const subscribe = (listener) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

export const useIsRead = (bookId) =>
  useSyncExternalStore(subscribe, () => ids.has(Number(bookId)), () => false);
