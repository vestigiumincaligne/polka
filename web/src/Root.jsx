import { t } from "./i18n";
import { useCallback, useEffect, useState } from "react";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";
import TopBar from "./components/TopBar";
import HomePage from "./pages/HomePage";
import BookPage from "./pages/BookPage";
import BookListPage from "./pages/BookListPage";
import SearchPage from "./pages/SearchPage";
import LoginPage from "./pages/LoginPage";
import AdminUsersPage from "./pages/AdminUsersPage";
import AdminLibraryPage from "./pages/AdminLibraryPage";
import ReaderPage from "./pages/ReaderPage";
import ListsPage from "./pages/ListsPage";
import SyncPage from "./pages/SyncPage";
import { fetchConfig } from "./api/fetchBooks";
import { fetchMe, logout } from "./api/auth";
import "./theme.css";
import "./Root.css";

const Root = () => {
  const [config, setConfig] = useState(null);
  const [user, setUser] = useState(null);
  const [authRequired, setAuthRequired] = useState(false);
  const [isDesktop, setIsDesktop] = useState(false);
  const [sync, setSync] = useState(null);
  const [authChecked, setAuthChecked] = useState(false);

  const loadConfig = useCallback(() => {
    fetchConfig()
      .then(setConfig)
      .catch(() => setConfig(null));
  }, []);

  useEffect(() => {
    fetchMe()
      .then((res) => {
        setUser(res.user ?? null);
        setAuthRequired(Boolean(res.authRequired));
        setIsDesktop(Boolean(res.desktop));
        setSync(res.sync ?? null);
      })
      .catch(() => {
        setUser(null);
        setAuthRequired(false);
      })
      .finally(() => setAuthChecked(true));
  }, []);

  useEffect(() => {
    if (!authChecked) return;
    if (!authRequired || user) loadConfig();
  }, [authChecked, authRequired, user, loadConfig]);

  const handleLogout = () => {
    logout().finally(() => {
      setUser(null);
      setConfig(null);
    });
  };

  if (!authChecked) {
    return <div style={{ padding: 48, textAlign: "center" }}>{t("loading")}</div>;
  }

  if (authRequired && !user) {
    return <LoginPage onLogin={setUser} />;
  }

  const layout = (children) => (
    <>
      <TopBar
        user={user}
        desktop={isDesktop}
        sync={sync}
        onLogout={isDesktop ? undefined : handleLogout}
      />
      <main className="root-main">{children}</main>
      <footer className="root-footer">
        <div className="container">{t("footer")}</div>
      </footer>
    </>
  );

  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={layout(<HomePage config={config} />)} />
        <Route path="/search" element={layout(<SearchPage config={config} />)} />
        <Route path="/lists" element={user ? layout(<ListsPage />) : <Navigate to="/" replace />} />
        <Route path="/book/:bookId" element={layout(<BookPage user={user} sync={sync} />)} />
        {(sync || isDesktop) && <Route path="/sync" element={layout(<SyncPage />)} />}
        {/* Reader has no shared header — it brings its own minimal UI */}
        <Route path="/read/:bookId" element={<ReaderPage />} />
        <Route path="/author/:authorId" element={layout(<BookListPage kind="author" />)} />
        <Route path="/series/:seriesId" element={layout(<BookListPage kind="series" />)} />
        <Route path="/shelf/:shelfId" element={layout(<BookListPage kind="shelf" />)} />
        <Route
          path="/admin/users"
          element={
            user?.role === "admin" && !isDesktop
              ? layout(<AdminUsersPage me={user} />)
              : <Navigate to="/" replace />
          }
        />
        <Route
          path="/admin/library"
          element={
            user?.role === "admin" ? layout(<AdminLibraryPage />) : <Navigate to="/" replace />
          }
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </HashRouter>
  );
};

export default Root;
