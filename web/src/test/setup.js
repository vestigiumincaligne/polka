import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";

// waitFor/findBy default to 1s, which slow CI runners overrun.
configure({ asyncUtilTimeout: 8000 });
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// jsdom has no ResizeObserver; Shelf uses it to toggle the scroll arrows.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = globalThis.ResizeObserver ?? ResizeObserverStub;
Element.prototype.scrollBy = Element.prototype.scrollBy ?? function () {};

afterEach(() => cleanup());
