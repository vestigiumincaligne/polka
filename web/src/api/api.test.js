import { describe, expect, it } from "vitest";
import api from "./api";

describe("buildUrl", () => {
  // In the embedded build VITE_API_URL is unset: URLs are same-origin.
  it("keeps relative paths and absolute URLs", () => {
    expect(api.buildUrl("main/getBooks/getConfig")).toBe("/main/getBooks/getConfig");
    expect(api.buildUrl("/opds")).toBe("/opds");
    expect(api.buildUrl("https://x.test/a")).toBe("https://x.test/a");
    expect(api.buildUrl("")).toBe("/");
  });
  it("derives book asset URLs from the id", () => {
    expect(api.coverUrl(7)).toBe("/Images/covers/7");
    expect(api.fb2Url(7)).toBe("/Images/fb2/7");
    expect(api.zipUrl(7)).toBe("/Images/zip/7");
    expect(api.fb2CompactUrl(7)).toBe("/Images/fb2compact/7");
  });
});
