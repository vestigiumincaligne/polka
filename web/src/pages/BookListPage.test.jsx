import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import BookListPage from "./BookListPage";
import { fetchAuthorBooks, fetchSeriesBooks, fetchShelfBooks } from "../api/fetchBooks";

vi.mock("../api/fetchBooks", () => ({
  fetchAuthorBooks: vi.fn(),
  fetchSeriesBooks: vi.fn(),
  fetchShelfBooks: vi.fn(),
}));
vi.mock("../api/manage", () => ({ exportUrl: (ids) => `/export?ids=${ids.join(",")}` }));

const book = (BookID, extras = {}) => ({ BookID, Title: `Book ${BookID}`, AuthorsNames: "Comma, Name", Authors: [{ AuthorID: 4, Name: "Comma, Name" }], ...extras });

function Navigation() {
  const navigate = useNavigate();
  return <button type="button" onClick={() => navigate("/author/next")}>Next author</button>;
}

function renderAuthor(id = "1") {
  return render(
    <MemoryRouter initialEntries={[`/author/${id}`]}>
      <Navigation />
      <Routes>
        <Route path="/author/:authorId" element={<BookListPage kind="author" />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("BookListPage browsing controls", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    fetchAuthorBooks.mockReset();
    fetchSeriesBooks.mockReset();
    fetchShelfBooks.mockReset();
  });

  it("filters an author page, groups series without colliding with the translated empty label, and exports only visible books", async () => {
    fetchAuthorBooks.mockResolvedValue({ titlesList: [
      book(1, { Lang: "en", SeriesTitle: "Alpha" }),
      book(2, { Lang: "ru", SeriesTitle: "Alpha" }),
      book(3, { Lang: "en", SeriesTitle: "Not in a series" }),
      book(4, { Lang: "en", SeriesTitle: "" }),
      book(5, { Lang: "en", SeriesTitle: "constructor" }),
      book(6, { Lang: "en", SeriesTitle: "__proto__" }),
      book(7, { Lang: "en", SeriesTitle: "toString" }),
    ] });
    renderAuthor();

    await screen.findByText("Book 1");
    fireEvent.change(screen.getByLabelText(/Language/i), { target: { value: "en" } });
    expect(screen.getByRole("link", { name: /download all/i })).toHaveAttribute("href", "/export?ids=1,3,4,5,6,7");
    expect(screen.queryByText("Book 2")).not.toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 2 })).toHaveLength(6);
  });

  it("shows no download link when filtering leaves no books and resets a stale language on author navigation", async () => {
    fetchAuthorBooks.mockResolvedValueOnce({ titlesList: [book(1, { Lang: "ru" })] })
      .mockResolvedValueOnce({ titlesList: [book(2, { Lang: "en" })] });
    const view = renderAuthor();
    await screen.findByText("Book 1");
    fireEvent.change(screen.getByLabelText(/Language/i), { target: { value: "ru" } });
    expect(screen.getByRole("link", { name: /download all/i })).toHaveAttribute("href", "/export?ids=1");
    fireEvent.click(screen.getByRole("button", { name: "Next author" }));
    await screen.findByText("Book 2");
    expect(screen.getByRole("link", { name: /download all/i })).toHaveAttribute("href", "/export?ids=2");
  });

  it("does not offer a bulk download for an empty result", async () => {
    fetchAuthorBooks.mockResolvedValue({ titlesList: [] });
    renderAuthor();
    await screen.findByText(/no books/i);
    expect(screen.queryByRole("link", { name: /download all/i })).not.toBeInTheDocument();
  });

  it("uses structured author IDs even when the author name contains a comma", async () => {
    fetchAuthorBooks.mockResolvedValue({ titlesList: [book(1)] });
    renderAuthor();
    const author = await screen.findByRole("link", { name: "Comma, Name" });
    expect(author).toHaveAttribute("href", "/author/4");
  });

  it("keeps rendering when remembered-view storage is denied", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied"); });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied"); });
    fetchAuthorBooks.mockResolvedValue({ titlesList: [book(1)] });
    renderAuthor();
    await screen.findByText("Book 1");
    fireEvent.click(screen.getByRole("button", { name: /list/i }));
    expect(screen.getByText("Book 1")).toBeInTheDocument();
  });
});
