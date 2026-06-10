import { getLang } from "../i18n";
import api from "./api";

const postForm = async (path, formData) => {
  const res = await fetch(api.buildUrl(path), {
    method: "POST",
    credentials: "include",
    headers: { "X-Polka-Lang": getLang() },
    body: formData,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

const post = async (path) => {
  const res = await fetch(api.buildUrl(path), { method: "POST", credentials: "include", headers: { "X-Polka-Lang": getLang() } });
  if (!res.ok) {
    const err = new Error(`Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const uploadBooks = (files, { force = false } = {}) => {
  const fd = new FormData();
  for (const f of files) fd.append("files", f);
  if (force) fd.append("force", "1");
  return postForm("admin/books/upload", fd);
};

export const deleteBook = (id) => post(`admin/books/${id}/delete`);
export const restoreBook = (id) => post(`admin/books/${id}/restore`);

export const importInpx = (file, replace) => {
  const fd = new FormData();
  fd.append("file", file);
  if (replace) fd.append("replace", "1");
  return postForm("admin/import/inpx", fd);
};

export const importStatus = () => api.getJson("admin/import/status");

export const exportUrl = (ids) => api.buildUrl(`Images/export?ids=${ids.join(",")}`);

export const fetchSettings = () => api.getJson("admin/settings");

export const saveSettings = async (settings) => {
  const res = await fetch(api.buildUrl("admin/settings"), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  if (!res.ok) throw new Error(`Request failed: ${res.status}`);
  return res.json();
};
