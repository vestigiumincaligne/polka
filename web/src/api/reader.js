import { getLang } from "../i18n";
import api from "./api";

export const fetchReadMeta = (bookId) => api.getJson(`api/v1/read/${bookId}`);

export const fetchChapter = (bookId, n) => api.getJson(`api/v1/read/${bookId}/chapter/${n}`);

export const fetchProgress = (bookId) => api.getJson(`api/v1/read/${bookId}/progress`);

export const saveProgress = (bookId, chapter, position, progress, locator = "") =>
  fetch(api.buildUrl(`api/v1/read/${bookId}/progress`), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify({ chapter, position, progress, locator }),
  }).catch(() => {});

// Fallback when the server does not store the position (public mode, no login).
export const localProgress = {
  get(bookId) {
    try {
      return JSON.parse(localStorage.getItem(`polka-read-${bookId}`));
    } catch {
      return null;
    }
  },
  set(bookId, chapter, position) {
    try {
      localStorage.setItem(`polka-read-${bookId}`, JSON.stringify({ chapter, position }));
    } catch {
      /* quota exceeded — no big deal */
    }
  },
};
