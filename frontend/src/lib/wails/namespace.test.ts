import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { BRIDGE_NAMESPACES } from "./bridge";

/**
 * The namespace gate.
 *
 * # The defect this exists to catch, which had shipped
 *
 * Wails publishes a bound struct under its Go PACKAGE name. The bindings live in
 * `internal/api/bindings`, so the runtime provides `window.go.bindings.Catalog.CreateProduct`.
 * `bridge.ts` read `window.go.main` — a name that was correct exactly once, when the only bound
 * struct was a placeholder in `main.go`, and was never re-checked after the bindings moved.
 *
 * The consequence was total and invisible: `hasBridge()` returned false inside the packaged
 * desktop app, so every call fell through to the BROWSER-DEV MOCK. The application looked like it
 * worked — it listed products, showed a signed-in user, rendered a dashboard — because the mock
 * answered every question. It was answering with fiction. The only visible symptom was that
 * actions the mock had no entry for ("add a product", "add a supplier") failed with "Mizan cannot
 * reach its backend", which read as a transport glitch rather than as the truth: nothing had ever
 * reached the backend.
 *
 * # Why no existing test could see it
 *
 * The Go tests call the bindings directly. The React tests install their own bridge. Both sides
 * were correct and thoroughly tested; the NAME connecting them was asserted by nobody.
 *
 * **A seam is only proven by a caller** — the finding this repository has now made seven times.
 *
 * # Why this reads the generated files
 *
 * `frontend/wailsjs/` is written by Wails itself from the real `Bind:` call. It is the one
 * artefact neither side can edit into agreement, which makes it the only honest oracle for what
 * the runtime will actually publish.
 */
const GENERATED = resolve(process.cwd(), "wailsjs/go");

/** Reads the namespace out of a generated call like `window['go']['bindings']['Auth']['Me']()`. */
function namespacesIn(source: string): Set<string> {
  const found = new Set<string>();
  for (const match of source.matchAll(/window\['go'\]\['([^']+)'\]/g)) {
    if (match[1]) found.add(match[1]);
  }
  return found;
}

describe("the Wails bridge namespace", () => {
  const packages = readdirSync(GENERATED, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);

  it("finds the files Wails generated", () => {
    // A gate whose input is missing passes while checking nothing — 7.6's D162, the failure this
    // whole file is a reaction to. If the generated directory ever moves, this fails loudly
    // rather than quietly certifying the namespace.
    expect(packages.length, `no generated packages under ${GENERATED}`).toBeGreaterThan(0);
  });

  it("reads every namespace the generated bindings actually call", () => {
    const published = new Set<string>();
    let scanned = 0;

    for (const pkg of packages) {
      const dir = resolve(GENERATED, pkg);
      for (const file of readdirSync(dir).filter((name) => name.endsWith(".js"))) {
        scanned += 1;
        for (const namespace of namespacesIn(readFileSync(resolve(dir, file), "utf8"))) {
          published.add(namespace);
        }
      }
    }

    expect(scanned, "no generated .js bindings were read").toBeGreaterThan(10);
    expect(published.size, "the generated bindings call no window.go namespace").toBeGreaterThan(0);

    for (const namespace of published) {
      expect(
        BRIDGE_NAMESPACES as readonly string[],
        `Wails publishes bound structs under window.go.${namespace}, and bridge.ts does not ` +
          `read that namespace. Every call would fall through to the browser-dev mock and the ` +
          `packaged application would never reach its backend.`,
      ).toContain(namespace);
    }
  });

  it("names the package the bindings actually live in", () => {
    // Belt and braces, and a readable failure: if the bindings package is ever renamed, this says
    // so in one line rather than leaving the reader to compare two sets.
    expect(packages).toContain("bindings");
    expect(BRIDGE_NAMESPACES as readonly string[]).toContain("bindings");
  });
});
