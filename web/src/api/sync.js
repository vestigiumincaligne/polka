import { getLang } from "../i18n";
import api from "./api";

const post = async (path) => {
  const res = await fetch(api.buildUrl(path), { method: "POST", credentials: "include", headers: { "X-Polka-Lang": getLang() } });
  if (!res.ok) {
    const err = new Error(`Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

const postJson = async (path, body) => {
  const res = await fetch(api.buildUrl(path), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text.trim() || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const fetchSyncInfo = () => api.getJson("api/v1/sync/info");
export const fetchSyncConfig = () => api.getJson("api/v1/sync/config");
export const saveSyncConfig = (cfg) => postJson("api/v1/sync/config", cfg);
export const disconnectSync = () => postJson("api/v1/sync/disconnect");
export const syncNow = () => post("api/v1/sync/now");
export const fetchOfflineBooks = () => api.getJson("api/v1/offline");
export const fetchOfflineStatus = (bookId) => api.getJson(`api/v1/offline/${bookId}`);
export const makeOffline = (bookId) => post(`api/v1/offline/${bookId}`);
export const removeOffline = (bookId) => post(`api/v1/offline/${bookId}/delete`);
