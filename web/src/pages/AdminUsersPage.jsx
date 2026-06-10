import { t } from "../i18n";
import { useEffect, useState } from "react";
import { createUser, deleteUser, fetchUsers, updateUser } from "../api/auth";
import "./AdminUsersPage.css";

const emptyForm = { login: "", password: "", displayName: "", role: "user" };

const AdminUsersPage = ({ me }) => {
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [form, setForm] = useState(emptyForm);
  const [busy, setBusy] = useState(false);

  const reload = () => {
    fetchUsers()
      .then((res) => setUsers(res.users ?? []))
      .catch((err) => setError(err))
      .finally(() => setLoading(false));
  };

  useEffect(reload, []);

  const showError = (err) => {
    if (err.status === 409) {
      alert(String(err.message).includes("admin") ? t("users.lastAdmin") : t("users.loginTaken"));
    } else {
      alert(t("users.opFail"));
    }
  };

  const submit = (e) => {
    e.preventDefault();
    if (!form.login || !form.password || busy) return;
    setBusy(true);
    createUser(form)
      .then(() => {
        setForm(emptyForm);
        reload();
      })
      .catch(showError)
      .finally(() => setBusy(false));
  };

  const toggleRole = (u) =>
    updateUser(u.id, { role: u.role === "admin" ? "user" : "admin" }).then(reload).catch(showError);

  const toggleDisabled = (u) =>
    updateUser(u.id, { disabled: !u.disabled }).then(reload).catch(showError);

  const resetPassword = (u) => {
    const password = prompt(t("users.passwordPrompt", { login: u.login }));
    if (!password) return;
    updateUser(u.id, { password }).then(() => alert(t("users.passwordDone"))).catch(showError);
  };

  const remove = (u) => {
    if (!confirm(t("users.deleteConfirm", { login: u.login }))) return;
    deleteUser(u.id).then(reload).catch(showError);
  };

  return (
    <div className="container users-page">
      <header className="users-page__header">
        <h1>{t("users.title")}</h1>
        <p className="users-page__hint">
          {t("users.hint")}
        </p>
      </header>

      {loading && <div>{t("loading")}</div>}
      {error && !loading && <div className="users-page__error">{t("users.loadFail")}</div>}

      {!loading && !error && (
        <div className="users-table-wrap">
        <table className="users-table">
          <thead>
            <tr>
              <th>{t("users.login")}</th>
              <th>{t("users.name")}</th>
              <th>{t("users.role")}</th>
              <th>{t("users.status")}</th>
              <th aria-label="actions" />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className={u.disabled ? "is-disabled" : ""}>
                <td>{u.login}{u.id === me?.id && <span className="tag users-table__you">{t("users.you")}</span>}</td>
                <td>{u.displayName || "—"}</td>
                <td>
                  <span className={`tag ${u.role === "admin" ? "users-table__admin" : ""}`}>
                    {u.role === "admin" ? t("users.admin") : t("users.reader")}
                  </span>
                </td>
                <td>{u.disabled ? t("users.disabled") : t("users.active")}</td>
                <td className="users-table__actions">
                  <button type="button" className="btn btn-link" onClick={() => resetPassword(u)}>
                    {t("users.password")}
                  </button>
                  <button type="button" className="btn btn-link" onClick={() => toggleRole(u)}>
                    {u.role === "admin" ? t("users.makeReader") : t("users.makeAdmin")}
                  </button>
                  <button type="button" className="btn btn-link" onClick={() => toggleDisabled(u)}>
                    {u.disabled ? t("users.enable") : t("users.disable")}
                  </button>
                  <button type="button" className="btn btn-link users-table__danger" onClick={() => remove(u)}>
                    {t("users.delete")}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
      )}

      <form className="users-form" onSubmit={submit}>
        <h2>{t("users.new")}</h2>
        <div className="users-form__row">
          <input
            type="text"
            placeholder={t("users.login")}
            autoComplete="off"
            value={form.login}
            onChange={(e) => setForm({ ...form, login: e.target.value })}
          />
          <input
            type="password"
            placeholder={t("users.password")}
            autoComplete="new-password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <input
            type="text"
            placeholder={t("users.namePh")}
            value={form.displayName}
            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
          />
          <select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
            <option value="user">{t("users.reader")}</option>
            <option value="admin">{t("users.admin")}</option>
          </select>
          <button type="submit" className="btn btn-primary" disabled={busy || !form.login || !form.password}>
            {t("users.create")}
          </button>
        </div>
      </form>
    </div>
  );
};

export default AdminUsersPage;
