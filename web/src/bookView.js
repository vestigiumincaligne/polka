const STORAGE_KEY = "polka-book-list-view";

export function readBookView() {
  try {
    return localStorage.getItem(STORAGE_KEY) === "list" ? "list" : "grid";
  } catch {
    return "grid";
  }
}

export function writeBookView(view) {
  try {
    localStorage.setItem(STORAGE_KEY, view);
  } catch {
    // Storage can be unavailable in private or restricted browser contexts.
  }
}
