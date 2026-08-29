import { describe, expect, it, vi } from "vitest";

// The module picks the language at import time, so each scenario loads a
// fresh copy with its own localStorage / navigator state.
const load = async ({ stored, navLang } = {}) => {
  vi.resetModules();
  localStorage.clear();
  if (stored) localStorage.setItem("polka-lang", stored);
  Object.defineProperty(navigator, "language", { value: navLang ?? "en-US", configurable: true });
  return import("./i18n.js");
};

describe("language detection", () => {
  it("falls back to the browser language", async () => {
    expect((await load({ navLang: "ru-RU" })).currentLang()).toBe("ru");
    expect((await load({ navLang: "de-DE" })).currentLang()).toBe("en");
    expect(document.documentElement.lang).toBe("en");
  });
  it("prefers the stored choice and ignores garbage", async () => {
    expect((await load({ stored: "ru", navLang: "en-US" })).currentLang()).toBe("ru");
    expect((await load({ stored: "xx", navLang: "en-US" })).currentLang()).toBe("en");
  });
  it("setLang stores the choice and reloads", async () => {
    const { setLang } = await load({ navLang: "en-US" });
    const reload = vi.fn();
    Object.defineProperty(window, "location", { value: { reload }, configurable: true });
    setLang("ru");
    expect(localStorage.getItem("polka-lang")).toBe("ru");
    expect(reload).toHaveBeenCalledOnce();
  });
});

describe("t()", () => {
  it("substitutes variables and falls back to the key", async () => {
    const { t } = await load({ stored: "en" });
    expect(t("admin.import.done", { books: 5, authors: 2, series: 1 })).toBe("Import finished: 5 books, 2 authors, 1 series.");
    expect(t("no.such.key")).toBe("no.such.key");
    expect(t("no.such.key", { n: 1 })).toBe("no.such.key");
  });
  it("uses the Russian text when the English one is missing", async () => {
    const { t } = await load({ stored: "en" });
    // Every key in the Russian dictionary must resolve to *something*.
    const ru = (await load({ stored: "ru" })).t;
    expect(ru("shelf.newest")).toBe("Новинки коллекции");
    expect(t("shelf.newest")).not.toBe("shelf.newest");
  });
  it("picks Russian plural forms", async () => {
    const { t } = await load({ stored: "ru" });
    const form = (n) => t("home.titleCount", { n });
    expect(form(1)).toBe("1 книга — найдите свою");
    expect(form(2)).toBe("2 книги — найдите свою");
    expect(form(5)).toBe("5 книг — найдите свою");
    expect(form(11)).toBe("11 книг — найдите свою");
    expect(form(21)).toBe("21 книга — найдите свою");
    expect(form(112)).toBe("112 книг — найдите свою");
    expect(form("578 612")).toBe("578 612 книг — найдите свою"); // pre-formatted numbers
  });
  it("picks English plural forms", async () => {
    const { t } = await load({ stored: "en" });
    expect(t("home.titleCount", { n: 1 })).toMatch(/^1 book\b/);
    expect(t("home.titleCount", { n: 2 })).toMatch(/^2 books\b/);
  });
});
