import { t, currentLang, setLang } from "../i18n";
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import "./TopBar.css";

const TopBar = ({ user, onLogout, desktop = false, sync = null }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();

  const [query, setQuery] = useState(searchParams.get("q") ?? "");

  useEffect(() => {
    setQuery(searchParams.get("q") ?? "");
  }, [searchParams]);

  const submit = (event) => {
    event.preventDefault();
    const trimmed = query.trim();
    if (trimmed.length < 3) return;
    navigate(`/search?q=${encodeURIComponent(trimmed)}`);
  };

  const isActive = (path) =>
    path === "/" ? location.pathname === "/" : location.pathname.startsWith(path);

  return (
    <header className="topbar">
      <div className="container topbar__row">
        <Link to="/" className="topbar__brand" aria-label={t("brand")}>
          <img className="topbar__brand-mark" src="/icons/logo.png" alt="" aria-hidden="true" />
          <span className="topbar__brand-text">
            <span className="topbar__brand-name">{t("brand")}</span>
          </span>
        </Link>

        <nav className="topbar__nav" aria-label="nav">
          <Link to="/" className={`topbar__nav-link ${isActive("/") ? "is-active" : ""}`}>
            {t("nav.home")}
          </Link>
          <Link
            to="/search?scope=bookTitles"
            className={`topbar__nav-link ${isActive("/search") ? "is-active" : ""}`}
          >
            {t("nav.catalog")}
          </Link>
          {user && (
            <Link to="/lists" className={`topbar__nav-link ${isActive("/lists") ? "is-active" : ""}`}>
              {t("nav.lists")}
            </Link>
          )}
          {(sync || desktop) && (
            <Link to="/sync" className={`topbar__nav-link ${isActive("/sync") ? "is-active" : ""}`}>
              {t("nav.server")}
              {sync && !sync.online && <span className="topbar__offline-dot" title={t("nav.serverDown")} />}
            </Link>
          )}
          {user?.role === "admin" && (
            <>
              <Link
                to="/admin/library"
                className={`topbar__nav-link ${isActive("/admin/library") ? "is-active" : ""}`}
              >
                {t("nav.manage")}
              </Link>
              {!desktop && (
                <Link
                  to="/admin/users"
                  className={`topbar__nav-link ${isActive("/admin/users") ? "is-active" : ""}`}
                >
                  {t("nav.users")}
                </Link>
              )}
            </>
          )}
        </nav>

        <form className="topbar__search" role="search" onSubmit={submit}>
          <span className="topbar__search-icon" aria-hidden="true">
            🔍
          </span>
          <input
            type="search"
            className="topbar__search-input"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("search.placeholder")}
            aria-label={t("search.aria")}
          />
          <button
            type="submit"
            className="topbar__search-submit"
            disabled={query.trim().length < 3}
          >
            {t("search.go")}
          </button>
        </form>

        <button
          type="button"
          className="topbar__lang"
          title="Language"
          onClick={() => setLang(currentLang() === "ru" ? "en" : "ru")}
        >
          {currentLang() === "ru" ? "EN" : "RU"}
        </button>

        {user && (
          <div className="topbar__user">
            <span className="topbar__user-name" title={user.login}>
              {desktop ? t("owner") : user.displayName || user.login}
            </span>
            {onLogout && (
              <button type="button" className="btn btn-link topbar__logout" onClick={onLogout}>
                {t("nav.logout")}
              </button>
            )}
          </div>
        )}
      </div>
    </header>
  );
};

export default TopBar;
