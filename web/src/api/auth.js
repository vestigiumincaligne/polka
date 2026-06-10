import { getLang } from "../i18n";
import api from "./api";

const postJson = async (path, body) => {
  const res = await fetch(api.buildUrl(path), {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-Polka-Lang": getLang() },
    body: JSON.stringify(body ?? {}),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || `Request failed: ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return res.json();
};

export const fetchMe = () => api.getJson("auth/me");

export const login = ({ login: user, password }) =>
  postJson("auth/login", { login: user, password });

export const logout = () => postJson("auth/logout");

export const fetchUsers = () => api.getJson("admin/users");

export const createUser = (payload) => postJson("admin/users", payload);

export const updateUser = (id, payload) => postJson(`admin/users/${id}`, payload);

export const deleteUser = (id) => postJson(`admin/users/${id}/delete`);
