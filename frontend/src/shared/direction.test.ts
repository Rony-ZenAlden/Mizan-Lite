import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The RTL gate (Step 10.3).
 *
 * # Why a source scan and not a screenshot
 *
 * This application is built for Arabic-first markets, so a layout that breaks in RTL is a layout
 * broken for the PRIMARY user — not an edge case.
 *
 * A visual check would need a browser this environment cannot drive, and a layout snapshot fails
 * on every legitimate change until somebody stops reading it. What actually causes RTL breakage is
 * mechanical and greppable: a physical-direction property where a logical one exists.
 *
 * `margin-left` is always the left in every language. `margin-inline-start` is the side the reader
 * begins on. The second is correct in both directions and costs nothing, so the rule has no
 * exceptions — which is what makes a blunt scan the right shape.
 *
 * The codebase passed this on the day it was written: nine logical properties, zero physical ones,
 * because every earlier phase used them. The gate exists so the NEXT screen cannot be the first.
 */

const ROOT = resolve(process.cwd(), "src");

/** Tailwind's physical utilities, and the logical utility that replaces each. */
const FORBIDDEN: Array<{ pattern: RegExp; instead: string }> = [
  { pattern: /\bm[lr]-[a-z0-9.[\]]+/g, instead: "ms-* / me-*" },
  { pattern: /\bp[lr]-[a-z0-9.[\]]+/g, instead: "ps-* / pe-*" },
  { pattern: /\btext-(left|right)\b/g, instead: "text-start / text-end" },
  { pattern: /\bborder-[lr]-[a-z0-9.[\]]+/g, instead: "border-s-* / border-e-*" },
  { pattern: /\brounded-[lr]-[a-z0-9.[\]]+/g, instead: "rounded-s-* / rounded-e-*" },
  { pattern: /\b(left|right)-[0-9]+\b/g, instead: "start-* / end-*" },
  // Raw CSS, for the stylesheet and any inline style object.
  { pattern: /margin-(left|right)\s*:/g, instead: "margin-inline-start / -end" },
  { pattern: /padding-(left|right)\s*:/g, instead: "padding-inline-start / -end" },
  { pattern: /text-align\s*:\s*(left|right)/g, instead: "text-align: start / end" },
  { pattern: /border-(left|right)\s*:/g, instead: "border-inline-start / -end" },
];

function sourceFiles(directory: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path));
      continue;
    }
    if (/\.(tsx?|css)$/.test(entry)) out.push(path);
  }
  return out;
}

describe("direction", () => {
  it("uses no physical-direction property where a logical one exists", () => {
    const files = sourceFiles(ROOT);

    // The scan reached the source. A walk that found nothing would pass while checking nothing —
    // the failure mode this project has caught three times in structural tests.
    expect(files.length).toBeGreaterThan(50);

    const offences: string[] = [];
    for (const file of files) {
      // This gate itself names every forbidden pattern, and so does its own test file.
      if (file.endsWith("direction.test.ts")) continue;

      const source = readFileSync(file, "utf8");
      for (const { pattern, instead } of FORBIDDEN) {
        for (const match of source.matchAll(pattern)) {
          offences.push(
            `${relative(ROOT, file)}: ${match[0]} — use ${instead}, which is correct in both directions`,
          );
        }
      }
    }

    expect(offences).toEqual([]);
  });
});
