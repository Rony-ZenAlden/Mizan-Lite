import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The WCAG AA contrast gate (Step 0.11 D5).
 *
 * This is the same species of check as Step 0.8's error-code coverage gate: a completeness
 * property over data, cheap to assert, and otherwise discovered by a customer squinting at a
 * receipt total. Dark-theme contrast in particular is where hand-picked palettes fail, and no
 * reviewer catches it by eye — so it is a test.
 *
 * It parses the real index.css rather than a duplicated copy of the palette, because a gate
 * that checks its own copy of the values proves nothing about what ships.
 */

// Resolved from the Vitest root (frontend/) rather than import.meta.url, which the jsdom
// environment rewrites to an http:// URL.
const CSS = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");

type RGB = [number, number, number];

/** Extracts the token block for a selector, then its `--color-*` declarations. */
function paletteFor(selector: string): Record<string, RGB> {
  const start = CSS.indexOf(selector);
  if (start === -1) throw new Error(`selector not found in index.css: ${selector}`);
  const open = CSS.indexOf("{", start);
  const close = CSS.indexOf("}", open);
  const block = CSS.slice(open + 1, close);

  const palette: Record<string, RGB> = {};
  for (const match of block.matchAll(/--color-([a-z-]+):\s*([\d\s]+);/g)) {
    const name = match[1];
    const value = match[2];
    if (!name || !value) continue;
    const channels = value.trim().split(/\s+/).map(Number);
    if (channels.length !== 3 || channels.some(Number.isNaN)) continue;
    palette[name] = channels as RGB;
  }
  return palette;
}

/** Relative luminance, per WCAG 2.1. */
function luminance([r, g, b]: RGB): number {
  const channel = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(a: RGB, b: RGB): number {
  const la = luminance(a);
  const lb = luminance(b);
  const hi = Math.max(la, lb);
  const lo = Math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

/** Looks a token up, failing loudly rather than silently comparing undefined. */
function token(palette: Record<string, RGB>, name: string): RGB {
  const value = palette[name];
  if (!value) throw new Error(`--color-${name} is not declared in index.css`);
  return value;
}

/** Text pairs must reach 4.5:1; UI affordances (borders, focus ring) 3:1. */
const TEXT_PAIRS: Array<[string, string]> = [
  ["text", "surface"],
  ["text", "surface-raised"],
  ["text", "surface-sunken"],
  ["text-muted", "surface"],
  ["text-muted", "surface-raised"],
  ["text-muted", "surface-sunken"],
  // Text on a solid accent fill.
  ["primary-fg", "primary"],
  ["danger-fg", "danger"],
  ["success-fg", "success"],
  ["warning-fg", "warning"],
  ["info-fg", "info"],
  // Accent text on its own tinted banner background.
  ["primary", "primary-subtle"],
  ["danger", "danger-subtle"],
  ["success", "success-subtle"],
  ["warning", "warning-subtle"],
  ["info", "info-subtle"],
  // Status text directly on the page.
  ["danger", "surface"],
  ["success", "surface"],
  ["warning", "surface"],
  ["info", "surface"],
];

const UI_PAIRS: Array<[string, string]> = [
  ["border-strong", "surface"],
  ["border-strong", "surface-raised"],
  ["ring", "surface"],
  ["ring", "surface-raised"],
  ["primary", "surface"],
];

const THEMES = {
  light: ":root {",
  // The dark block overrides only some tokens, so it is merged over light.
  dark: ':root[data-theme="dark"] {',
} as const;

describe.each(Object.entries(THEMES))("%s theme contrast", (themeName, selector) => {
  const palette =
    themeName === "light"
      ? paletteFor(selector)
      : { ...paletteFor(THEMES.light), ...paletteFor(selector) };

  it.each(TEXT_PAIRS)("%s on %s meets AA for text (4.5:1)", (fg, bg) => {
    const ratio = contrast(token(palette, fg), token(palette, bg));
    expect(
      Number(ratio.toFixed(2)),
      `--color-${fg} on --color-${bg} in the ${themeName} theme is ${ratio.toFixed(2)}:1`,
    ).toBeGreaterThanOrEqual(4.5);
  });

  it.each(UI_PAIRS)("%s on %s meets AA for UI affordances (3:1)", (fg, bg) => {
    const ratio = contrast(token(palette, fg), token(palette, bg));
    expect(
      Number(ratio.toFixed(2)),
      `--color-${fg} on --color-${bg} in the ${themeName} theme is ${ratio.toFixed(2)}:1`,
    ).toBeGreaterThanOrEqual(3);
  });
});

describe("token completeness", () => {
  it("declares every colour token in both themes or inherits it deliberately", () => {
    const light = Object.keys(paletteFor(THEMES.light));
    const dark = Object.keys(paletteFor(THEMES.dark));

    // Dark may legitimately inherit a token; it may NOT introduce one light lacks, because
    // that token would be undefined in the light theme and render as nothing.
    const darkOnly = dark.filter((t) => !light.includes(t));
    expect(darkOnly, "tokens declared only in the dark theme").toEqual([]);
  });

  it("has no focus treatment that removes the outline", () => {
    // An invisible focus ring makes the app unusable by keyboard, and a POS is keyboard-first
    // (§31). `outline: none` without a replacement is the single most common way that happens.
    expect(CSS).not.toMatch(/outline:\s*none/);
  });
});
