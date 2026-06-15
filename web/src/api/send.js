import { getLang } from "../i18n";
import api from "./api";

const req = async (path, method = "GET", body) => {
  const res = await fetch(api.buildUrl(path), {
    method,
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const fetchReaderEmail = () => req("api/v1/me/reader-email");
export const setReaderEmail = (email) => req("api/v1/me/reader-email", "POST", { email });
export const sendBook = (bookId) => req(`api/v1/books/${bookId}/send`, "POST");
