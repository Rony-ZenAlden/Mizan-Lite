import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { SRC, productionFiles, rel } from "./source";

const css = readFileSync(join(SRC, "index.css"), "utf8");

describe("design tokens", () => {
  it("the font stack ends in a generic family and names an Arabic-capable face on each platform", () => {
    // Mizan 10.17: a stack of fonts no machine had, with no generic terminator, rendered in Times.
    const stack = /--font-sans:\s*([^;]+);/.exec(css)?.[1]?.replace(/\s+/g, " ").trim() ?? "";
    expect(stack).toMatch(/sans-serif$/);
    expect(stack).toContain('"Segoe UI"'); // Windows, which covers Arabic
    expect(stack).toMatch(/"SF Arabic"|"Geeza Pro"/); // macOS
  });

  it("no component holds a raw colour", () => {
    const offenders = productionFiles()
      .filter((f) => f.endsWith(".tsx"))
      .filter((f) => /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/.test(readFileSync(f, "utf8")))
      .map(rel);
    expect(offenders).toEqual([]);
  });
});
