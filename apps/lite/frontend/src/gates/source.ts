// Shared helpers for the gates: read the real source tree and parse it with the TypeScript compiler.
// Gates read files, not modules, because what they assert is about the text a build is made from.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import ts from "typescript";

/**
 * The frontend root. Vitest runs from the package directory (npm scripts do); the guard below makes a
 * run from anywhere else fail loudly instead of scanning the wrong tree and passing.
 */
export const FRONTEND = resolve(process.cwd());
const manifest = JSON.parse(readFileSync(join(FRONTEND, "package.json"), "utf8")) as { name?: string };
if (manifest.name !== "mizan-lite-frontend") {
  throw new Error(`gates must run from apps/lite/frontend; cwd is ${FRONTEND}`);
}
export const SRC = join(FRONTEND, "src");
export const GENERATED = join(FRONTEND, "wailsjs", "go", "api");

export function walk(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    else out.push(full);
  }
  return out;
}

export const isTest = (file: string) => /\.(test|gates\.test|bundle\.test)\.tsx?$/.test(file);

/** Production source: .ts/.tsx under src, excluding tests, the test helpers and the gates themselves. */
export function productionFiles(): string[] {
  return walk(SRC).filter(
    (f) =>
      /\.tsx?$/.test(f) &&
      !isTest(f) &&
      !f.endsWith(join("api", "testing.tsx")) &&
      !f.includes(join("src", "gates")) &&
      !f.includes(join("src", "test")),
  );
}

export function parse(file: string): ts.SourceFile {
  return ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true,
    file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
}

export function visit(node: ts.Node, fn: (n: ts.Node) => void): void {
  fn(node);
  node.forEachChild((child) => visit(child, fn));
}

export const rel = (file: string) => relative(FRONTEND, file);
