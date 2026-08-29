import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { t } from "../i18n";
import HomePage from "./HomePage";

vi.mock("../api/fetchBooks", () => ({ fetchHomeShelves: vi.fn() }));
import { fetchHomeShelves } from "../api/fetchBooks";

const Location = () => {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname}</div>;
};

const renderHome = (config) =>
  render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route path="/" element={<HomePage config={config} />} />
        <Route path="*" element={<Location />} />
      </Routes>
    </MemoryRouter>
  );

// Each test sets its own return value; the mock is never reset — in
// vitest 4 a reset/clear followed by a rejected return value is reported
// as a test error even though the component handles the rejection.
describe("HomePage", () => {
  it("renders built-in, personal and collection shelves", async () => {
    fetchHomeShelves.mockResolvedValue({
      shelves: [
        { id: "reading", books: [{ BookID: 1, Title: "Читаю", ReadingProgress: 0.5 }], hasMore: false },
        { id: "newest", books: [{ BookID: 2, Title: "Новинка" }], hasMore: true },
        {
          id: "collection_le-monde-100", title: "100 книг Le Monde", subtitle: "В библиотеке 79 из 100",
          books: [{ BookID: 3, Title: "Посторонний" }], hasMore: true,
        },
      ],
    });
    renderHome({ numberOfBooks: 1234, groups: [] });
    expect(await screen.findByText("Читаю")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: t("shelf.reading") })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: t("shelf.newest") })).toBeInTheDocument();
    // Collections come with their own title and subtitle from the server.
    expect(screen.getByRole("heading", { name: "100 книг Le Monde" })).toBeInTheDocument();
    expect(screen.getByText("В библиотеке 79 из 100")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(/1[,  ]?234/);

    // "See all" opens the shelf page for shelves that have more books.
    const seeAll = screen.getAllByRole("button", { name: t("shelf.seeAll") });
    expect(seeAll).toHaveLength(2); // newest + collection; "reading" has no more
    fireEvent.click(seeAll[1]);
    expect(await screen.findByTestId("location")).toHaveTextContent("/shelf/collection_le-monde-100");
  });

  it("sends the wishlist shelf to the lists page", async () => {
    fetchHomeShelves.mockResolvedValue({
      shelves: [{ id: "wishlist", books: [{ BookID: 1, Title: "Хочу" }], hasMore: false }],
    });
    renderHome({});
    await screen.findByText("Хочу");
    fireEvent.click(screen.getByRole("button", { name: t("shelf.seeAll") }));
    expect(await screen.findByTestId("location")).toHaveTextContent("/lists");
  });

  it("shows the empty state", async () => {
    fetchHomeShelves.mockResolvedValue({ shelves: [] });
    renderHome({});
    expect(await screen.findByText(t("home.empty"))).toBeInTheDocument();
  });

  it("shows the error state", async () => {
    // A rejected promise that is already observed: vitest would otherwise
    // flag the rejection before the component attaches its handler.
    const failed = Promise.reject(new Error("boom"));
    failed.catch(() => {});
    fetchHomeShelves.mockReturnValue(failed);
    renderHome({});
    await waitFor(() => expect(document.querySelector(".homepage__error")).not.toBeNull());
    expect(document.querySelector(".homepage__error")).toHaveTextContent(t("home.loadError"));
  });
});
