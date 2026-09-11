import { t } from "../i18n";
import { useEffect, useState } from "react";
import { fetchKosync, setKosyncPassword } from "../api/kosync";
import "./KosyncDialog.css";

// Device sync (KOReader/kosync) setup: the user sets a device password
// here and enters the server address + Polka login on the e-reader.
const KosyncDialog = ({ onClose }) => {
  const [enabled, setEnabled] = useState(null);
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [msg, setMsg] = useState(null);
  const serverUrl = window.location.origin + "/";

  useEffect(() => {
    fetchKosync()
      .then((res) => {
        setEnabled(Boolean(res.enabled));
        setLogin(res.login ?? "");
      })
      .catch(() => setEnabled(false));
  }, []);

  const save = () => {
    if (password.length < 8) {
      setMsg({ ok: false, text: t("kosync.tooShort") });
      return;
    }
    setKosyncPassword(password)
      .then(() => {
        setEnabled(true);
        setPassword("");
        setMsg({ ok: true, text: t("kosync.saved") });
      })
      .catch(() => setMsg({ ok: false, text: t("kosync.fail") }));
  };

  const disable = () => {
    setKosyncPassword("")
      .then(() => {
        setEnabled(false);
        setMsg({ ok: true, text: t("kosync.disabled") });
      })
      .catch(() => setMsg({ ok: false, text: t("kosync.fail") }));
  };

  return (
    <div className="kosync__overlay" onClick={onClose} role="presentation">
      <div className="kosync__card" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={t("kosync.title")}>
        <h2>{t("kosync.title")}</h2>
        <p className="kosync__hint">{t("kosync.hint")}</p>
        <ol className="kosync__steps">
          <li>
            {t("kosync.step.server")} <code>{serverUrl}</code>
          </li>
          <li>
            {t("kosync.step.login")} <code>{login}</code>
          </li>
          <li>{t("kosync.step.password")}</li>
        </ol>
        <div className="kosync__row">
          <input
            type="password"
            placeholder={enabled ? t("kosync.newPassword") : t("kosync.password")}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && save()}
          />
          <button type="button" className="btn btn-primary" onClick={save}>
            {t("kosync.save")}
          </button>
        </div>
        <div className="kosync__row kosync__row--status">
          <span className={enabled ? "kosync__on" : "kosync__off"}>
            {enabled === null ? t("loading") : enabled ? t("kosync.on") : t("kosync.off")}
          </span>
          {enabled && (
            <button type="button" className="btn btn-ghost" onClick={disable}>
              {t("kosync.disable")}
            </button>
          )}
        </div>
        {msg && <div className={msg.ok ? "kosync__ok" : "kosync__error"}>{msg.text}</div>}
        <div className="kosync__row kosync__row--close">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            {t("kosync.close")}
          </button>
        </div>
      </div>
    </div>
  );
};

export default KosyncDialog;
