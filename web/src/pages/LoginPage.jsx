import { t } from "../i18n";
import { useState } from "react";
import { login } from "../api/auth";
import "./LoginPage.css";

const LoginPage = ({ onLogin }) => {
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const submit = (e) => {
    e.preventDefault();
    if (!user || !password || busy) return;
    setBusy(true);
    setError(null);
    login({ login: user, password })
      .then((res) => onLogin(res.user))
      .catch((err) => {
        setError(err.status === 401 ? t("login.bad") : t("login.fail"));
      })
      .finally(() => setBusy(false));
  };

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={submit}>
        <img className="login-card__logo" src="/icons/logo.png" alt="" aria-hidden="true" />
        <h1 className="login-card__title">{t("brand")}</h1>
        <p className="login-card__subtitle">{t("login.subtitle")}</p>

        <label className="login-card__field">
          <span>{t("login.login")}</span>
          <input
            type="text"
            autoComplete="username"
            autoFocus
            value={user}
            onChange={(e) => setUser(e.target.value)}
          />
        </label>

        <label className="login-card__field">
          <span>{t("login.password")}</span>
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>

        {error && <div className="login-card__error">{error}</div>}

        <button type="submit" className="btn btn-primary login-card__submit" disabled={busy}>
          {busy ? t("login.busy") : t("login.submit")}
        </button>
      </form>
    </div>
  );
};

export default LoginPage;
