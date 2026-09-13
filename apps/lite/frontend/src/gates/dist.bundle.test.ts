import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { FAKE_CLIENT_SENTINEL } from "@/api/sentinel";

const DIST = resolve(process.cwd(), "dist");

// G5, against the BUILT artefact. Runs after `vite build`, in its own invocation (npm run test:bundle).
describe("G5 — the packaged frontend", () => {
  it("was built (a gate with no input checks nothing)", () => {
    expect(existsSync(join(DIST, "assets")), "run `npm run build` before `npm run test:bundle`").toBe(true);
  });

  const scripts = () =>
    readdirSync(join(DIST, "assets"))
      .filter((f) => f.endsWith(".js"))
      .map((f) => readFileSync(join(DIST, "assets", f), "utf8"));

  it("contains no trace of the test-only fake client", () => {
    for (const js of scripts()) expect(js.includes(FAKE_CLIENT_SENTINEL)).toBe(false);
  });

  it("calls the generated bindings under window.go.api", () => {
    const joined = scripts().join("\n");
    // Minification may rewrite window['go']['api'] as window.go.api; either form is the real bridge.
    expect(joined).toMatch(/window(\.go|\[["']go["']\])(\.api|\[["']api["']\])/);
  });

  it("serves index.html with the tag Go rewrites to the stored language", () => {
    const html = readFileSync(join(DIST, "index.html"), "utf8");
    expect(html.split('<html lang="ar" dir="rtl">').length - 1).toBe(1);
  });
});
