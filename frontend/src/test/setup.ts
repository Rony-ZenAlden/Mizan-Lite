import "@testing-library/jest-dom/vitest";
import { afterEach, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import { clearBridge } from "@/lib/wails/testing";

// Radix measures elements and uses APIs jsdom does not implement. Stubbing them here rather
// than per-test keeps the primitive tests about behaviour instead of about jsdom.
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
}

if (!window.ResizeObserver) {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof window.ResizeObserver;
}

if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => undefined;
  Element.prototype.releasePointerCapture = () => undefined;
}

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => undefined;
}

afterEach(() => {
  cleanup();
  // Every test starts with no Wails bridge; a test that wants one installs it explicitly.
  clearBridge();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("dir");
  document.documentElement.removeAttribute("lang");
  vi.restoreAllMocks();
});
