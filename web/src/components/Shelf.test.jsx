import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { t } from "../i18n";
import Shelf from "./Shelf";

const books = [
  { BookID: 1, Title: "Первая" },
  { BookID: 2, Title: "Вторая" },
];

describe("Shelf", () => {
  it("renders nothing when empty and without a hint", () => {
    const { container } = render(<MemoryRouter><Shelf title="X" books={[]} /></MemoryRouter>);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the hint for an empty shelf", () => {
    render(<MemoryRouter><Shelf title="X" books={[]} emptyHint="Пока пусто" /></MemoryRouter>);
    expect(screen.getByText("Пока пусто")).toBeInTheDocument();
  });

  it("renders title, subtitle, cards and the see-all button", () => {
    const onSeeAll = vi.fn();
    render(<MemoryRouter><Shelf title="Полка" subtitle="В библиотеке 2 из 3" books={books} onSeeAll={onSeeAll} /></MemoryRouter>);
    expect(screen.getByRole("heading", { name: "Полка" })).toBeInTheDocument();
    expect(screen.getByText("В библиотеке 2 из 3")).toBeInTheDocument();
    expect(screen.getByText("Первая")).toBeInTheDocument();
    expect(screen.getByText("Вторая")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: t("shelf.seeAll") }));
    expect(onSeeAll).toHaveBeenCalledOnce();
  });

  it("has no see-all button without a handler", () => {
    render(<MemoryRouter><Shelf title="Полка" books={books} /></MemoryRouter>);
    expect(screen.queryByRole("button", { name: t("shelf.seeAll") })).toBeNull();
  });
});
