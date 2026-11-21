import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import BookPage from "./BookPage";

vi.mock("../api/fetchBooks", () => ({
  fetchBookForm: vi.fn(),
  fetchExternalEnrichment: vi.fn(() => Promise.resolve(null)),
  fetchSimilarBooks: vi.fn(() => Promise.resolve({ similar: [], external: [] })),
}));
vi.mock("../api/reader", () => ({ fetchProgress: vi.fn(() => Promise.resolve({})) }));
vi.mock("../api/send", () => ({
  fetchReaderEmail: vi.fn(() => Promise.resolve({})),
  setReaderEmail: vi.fn(),
  sendBook: vi.fn(),
}));
vi.mock("../api/lists", () => ({
  fetchLists: vi.fn(() => Promise.resolve({ lists: [] })),
  toggleWishlist: vi.fn(),
  setRead: vi.fn(),
  fetchReadIds: vi.fn(() => Promise.resolve({ ids: [] })),
}));
import { fetchBookForm } from "../api/fetchBooks";
import { fetchLists, setRead } from "../api/lists";
import { fireEvent, waitFor } from "@testing-library/react";

const renderBook = () =>
  render(
    <MemoryRouter initialEntries={["/book/7"]}>
      <Routes>
        <Route path="/book/:bookId" element={<BookPage user={null} sync={false} />} />
      </Routes>
    </MemoryRouter>
  );

const book = (series) => ({
  bookForm: { BookID: 7, Title: "Ночной Дозор", Ext: "fb2", Genres: "" },
  authors: [{ AuthorID: 3, LastName: "Лукьяненко", FirstName: "Сергей" }],
  series,
});

describe("BookPage read toggle", () => {
  it("marks read, drops the wishlist mark and unmarks", async () => {
    fetchLists.mockResolvedValue({ lists: [{ id: 1, builtin: "wishlist" }, { id: 2, builtin: "read" }] });
    fetchBookForm.mockResolvedValue({ ...book([]), bookListIds: [1] });
    setRead.mockResolvedValueOnce({ ok: true, read: true, listId: 2 }).mockResolvedValueOnce({ ok: true, read: false, listId: 2 });
    renderBook();
    const button = await screen.findByRole("button", { name: /Прочитано|Read/ });
    await waitFor(() => expect(document.querySelector(".book-page__wish.is-on")).not.toBeNull()); // in wishlist
    fireEvent.click(button);
    await waitFor(() => expect(button.className).toContain("is-on"));
    expect(setRead).toHaveBeenLastCalledWith("7", true);
    expect(document.querySelectorAll(".book-page__wish.is-on")).toHaveLength(1); // only "Read" is on
    fireEvent.click(button);
    await waitFor(() => expect(button.className).not.toContain("is-on"));
    expect(setRead).toHaveBeenLastCalledWith("7", false);
  });
});
