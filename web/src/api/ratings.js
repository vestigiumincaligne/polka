import { getLang } from "../i18n";
import api from "./api";

export const rateBook = async (bookId, rating) => {
  const res = await fetch(api.buildUrl(`api/v1/books/${bookId}/rating`), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify({ rating }),
  });
  if (!res.ok) {
    const err = new Error(`Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};
