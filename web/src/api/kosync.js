import { getLang } from "../i18n";
import api from "./api";

export const fetchKosync = () => api.getJson("api/v1/me/kosync");

export const setKosyncPassword = async (password) => {
  const res = await fetch(api.buildUrl("api/v1/me/kosync"), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify({ password }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};
