import { getLang } from "../i18n";
import api from "./api";

const post = async (path, body) => {
  const res = await fetch(api.buildUrl(path), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) {
    const err = new Error(`Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const fetchLists = () => api.getJson("api/v1/lists");

export const createList = (name) => post("api/v1/lists", { name });

export const renameList = (id, name) => post(`api/v1/lists/${id}`, { name });

export const deleteList = (id) => post(`api/v1/lists/${id}/delete`);

export const fetchListBooks = (id, { limit, offset } = {}) => {
  const qs = new URLSearchParams();
  if (limit) qs.set("limit", limit);
  if (offset) qs.set("offset", offset);
  const tail = qs.toString() ? `?${qs}` : "";
  return api.getJson(`api/v1/lists/${id}/books${tail}`);
};

export const addToList = (listId, bookId) => post(`api/v1/lists/${listId}/books`, { bookId });

export const removeFromList = (listId, bookId) =>
  post(`api/v1/lists/${listId}/books/remove`, { bookId });

export const toggleWishlist = (bookId, add) => post(`api/v1/books/${bookId}/wishlist`, { add });
