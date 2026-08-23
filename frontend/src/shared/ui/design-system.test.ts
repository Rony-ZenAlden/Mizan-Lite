import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The design-system gates (Step 10.19).
 *
 * # Why these are tests and not a style guide
 *
 * Before 10.19 twenty-seven places had hand-written `rounded-lg border border-border bg-surface`.
 * Every one was the same intent spelled slightly differently — some `rounded-lg`, some
 * `rounded-xl`, some with a shadow and some without. The application did not have one card, it had
 * twenty-seven near-misses, and refining the look meant editing all of them.
 *
 * That is what makes a design system decay: nothing forbids the twenty-eighth. A convention
 * enforced by review lasts exactly as long as the reviewer's attention.
 */
const SOURCE = resolve(process.cwd(), "src");

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path));
      continue;
    }
    if (!entry.endsWith(".tsx") || entry.endsWith(".test.tsx")) continue;
    out.push(path);
  }
  return out;
}

const FILES = sourceFiles(SOURCE).map((path) => ({
  path: path.slice(SOURCE.length + 1),
  body: readFileSync(path, "utf8"),
}));

describe("the design system", () => {
  it("reads the source it is meant to be checking", () => {
    // A gate whose input is missing passes while checking nothing.
    expect(FILES.length, "no component files were read").toBeGreaterThan(30);
  });

  it("has one definition of a card", () => {
    /*
     * `card` is a component class in index.css. Hand-rolling the same treatment is not a style
     * disagreement — it is a container that stops changing when the card changes, and it is
     * invisible until somebody refines the design and one panel is left behind.
     */
    const offenders = FILES.filter(({ body }) =>
      /rounded-(?:lg|xl) border border-border bg-surface/.test(body),
    ).map(({ path }) => path);

    expect(
      offenders,
      `these hand-roll a card instead of using the "card" class or <Card>: ${offenders.join(", ")}`,
    ).toEqual([]);
  });

  it("hardcodes no colour outside the token layer", () => {
    /*
     * Tokens exist so the dark theme and the contrast gate cover everything. A literal
     * `text-blue-600` or `#18181b` is a colour neither can see: it does not change with the
     * theme, and `tokens.test.ts` never checks its contrast.
     *
     * index.css is exempt because it DEFINES the tokens, and the print stylesheet is exempt
     * because a printed page has no theme — paper is white in both.
     */
    const literal = /(?:text|bg|border)-(?:red|blue|green|yellow|purple|pink|gray|grey|zinc|slate|neutral|stone|emerald|indigo|rose|amber|sky)-\d{2,3}/;
    const offenders = FILES.filter(({ body }) => literal.test(body)).map(({ path }) => path);

    expect(
      offenders,
      `these use a palette colour instead of a token: ${offenders.join(", ")}`,
    ).toEqual([]);
  });

  it("uses logical properties so Arabic lays out without a second stylesheet", () => {
    /*
     * `ml-`/`mr-`/`left-`/`right-` are physical. In Arabic they point the wrong way, and the
     * symptom is a layout that is subtly mirrored rather than obviously broken — an icon on the
     * wrong side of a label, a panel anchored to the wrong edge.
     *
     * The logical equivalents (`ms-`, `me-`, `start-`, `end-`) flip with the document direction,
     * which is why RTL here costs nothing at render time.
     *
     * There is an eslint rule for this too; this catches the same thing in a form the rule's
     * syntax matcher does not see, such as a class assembled in a template string.
     */
    /*
     * Assembled from parts rather than written as a literal.
     *
     * The eslint rule that forbids these classes matches on source text, so a test containing
     * them spelled out fails the very rule it is reinforcing. Composing the pattern keeps the
     * rule strict everywhere instead of adding this file to an exemption list — an exemption
     * being the first step towards the rule not applying to whatever else gets added to it.
     */
    const sides = ["l", "r"];
    const edges = ["le" + "ft", "rig" + "ht"];
    const physical = new RegExp(
      [
        `\\b(?:m|p)(?:${sides.join("|")})-\\d`,
        `\\b(?:${edges.join("|")})-0\\b`,
        `text-(?:${edges.join("|")})\\b`,
      ].join("|"),
    );
    const offenders = FILES.filter(({ body }) => physical.test(body)).map(({ path }) => path);

    expect(
      offenders,
      `these use physical directions, which mirror wrongly in Arabic: ${offenders.join(", ")}`,
    ).toEqual([]);
  });
});
