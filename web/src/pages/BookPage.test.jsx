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
}));
import { fetchBookForm } from "../api/fetchBooks";

const renderBook = (config) =>
  render(
    <MemoryRouter initialEntries={["/book/7"]}>
      <Routes>
        <Route path="/book/:bookId" element={<BookPage user={null} sync={false} config={config} />} />
      </Routes>
    </MemoryRouter>
  );

const fb2Book = {
  bookForm: { BookID: 7, Title: "Ночной Дозор", Ext: ".fb2", Genres: "" },
  authors: [{ AuthorID: 3, LastName: "Лукьяненко", FirstName: "Сергей" }],
  series: [],
};

const epubLink = () => document.querySelector('a[href$="/Images/epub/7"]');

describe("BookPage EPUB download", () => {
  it("offers EPUB for FB2 books when the server can convert", async () => {
    fetchBookForm.mockResolvedValue(fb2Book);
    renderBook({ epubDownloads: true });
    await screen.findByRole("link", { name: "Лукьяненко Сергей" });
    expect(epubLink()).not.toBeNull();
  });

  it("hides EPUB when Pandoc is not available", async () => {
    fetchBookForm.mockResolvedValue(fb2Book);
    renderBook({ epubDownloads: false });
    await screen.findByRole("link", { name: "Лукьяненко Сергей" });
    expect(epubLink()).toBeNull();
  });
});
