import { getLang } from "../i18n";
import api from "./api";

const post = async (path, body) => {
  const res = await fetch(api.buildUrl(path), {
    method: "POST",
    credentials: "include",
    headers: { "X-Polka-Lang": getLang() },
    body,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const fetchCollections = () => api.getJson("api/v1/collections");

export const fetchCollection = (slug) => api.getJson(`api/v1/collections/${encodeURIComponent(slug)}`);

export const importCollection = (file) => {
  const fd = new FormData();
  fd.append("file", file);
  return post("admin/collections/import", fd);
};

export const rematchCollection = (slug) => post(`admin/collections/${encodeURIComponent(slug)}/match`);

export const deleteCollection = (slug) => post(`admin/collections/${encodeURIComponent(slug)}/delete`);

export const syncCollectionSource = (id) => post(`admin/collections/sources/${encodeURIComponent(id)}/sync`);
