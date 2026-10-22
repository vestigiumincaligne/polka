import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import SearchPage from "./SearchPage";
import { fetchSearchStats, fetchSearchTitles } from "../api/fetchBooks";

vi.mock("../api/fetchBooks", () => ({
  fetchCatalogShelves: vi.fn(),
  fetchSearchAuthors: vi.fn(),
  fetchSearchGenres: vi.fn(),
  fetchSearchSeries: vi.fn(),
  fetchSearchStats: vi.fn(),
  fetchSearchTitles: vi.fn(),
}));

function renderSearch() {
  return render(
    <MemoryRouter initialEntries={["/search?q=polka"]}>
      <Routes><Route path="/search" element={<SearchPage config={{}} />} /></Routes>
    </MemoryRouter>,
  );
}

describe("SearchPage book view", () => {
  beforeEach(() => {
    localStorage.clear();
    fetchSearchStats.mockResolvedValue({ searchStats: { bookTitles: 1 } });
    fetchSearchTitles.mockResolvedValue({ titlesList: [{ BookID: 8, Title: "A result" }] });
  });

  it("persists the list choice for a later search page mount", async () => {
    const first = renderSearch();
    const listButton = await screen.findByRole("button", { name: "List" });
    fireEvent.click(listButton);
    expect(screen.getByRole("article")).toHaveClass("book-card--list");
    first.unmount();
    renderSearch();
    await screen.findByRole("article");
    expect(screen.getByRole("article")).toHaveClass("book-card--list");
  });
});
