import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";
import { afterEach, describe, expect, it } from "vitest";
import { createClient } from "@/api/client";
import { CODE_BRIDGE_UNAVAILABLE } from "@/api/envelope";
import { GENERATED, SRC, parse, productionFiles, rel, visit } from "./source";

const CLIENT = join(SRC, "api", "client.ts");

/** `group.method` for every function the client exposes, read from createClient's returned object. */
function clientShape(): string[] {
  const out: string[] = [];
  visit(parse(CLIENT), (node) => {
    if (!ts.isFunctionDeclaration(node) || node.name?.text !== "createClient") return;
    visit(node, (inner) => {
      if (!ts.isReturnStatement(inner) || !inner.expression || !ts.isObjectLiteralExpression(inner.expression)) return;
      for (const group of inner.expression.properties) {
        if (!ts.isPropertyAssignment(group) || !ts.isObjectLiteralExpression(group.initializer)) continue;
        for (const method of group.initializer.properties) {
          if (method.name && ts.isIdentifier(method.name) && ts.isIdentifier(group.name)) {
            out.push(`${group.name.text}.${method.name.text}`);
          }
        }
      }
    });
  });
  return out;
}

/** Every `x.group.method` / `group.method` property access in a file — from the AST, so comments do not count. */
function referencesIn(file: string): Set<string> {
  const found = new Set<string>();
  visit(parse(file), (node) => {
    if (!ts.isPropertyAccessExpression(node)) return;
    const target = node.expression;
    const group = ts.isPropertyAccessExpression(target)
      ? target.name.text
      : ts.isIdentifier(target)
        ? target.text
        : undefined;
    if (group) found.add(`${group}.${node.name.text}`);
  });
  return found;
}

function generatedModules(): string[] {
  return readdirSync(GENERATED)
    .filter((f) => f.endsWith(".d.ts"))
    .map((f) => f.replace(/\.d\.ts$/, ""));
}

function generatedFunctions(module: string): string[] {
  const source = readFileSync(join(GENERATED, `${module}.d.ts`), "utf8");
  return [...source.matchAll(/export function (\w+)\(/g)].map((m) => m[1]!);
}

describe("G1 — nothing but the client reaches the bridge", () => {
  it("finds the generated bindings (a gate with no input checks nothing)", () => {
    const modules = generatedModules();
    expect(modules.length, `no generated modules under ${GENERATED}; run \`wails generate module\``).toBeGreaterThanOrEqual(2);
  });

  it("only src/api/client.ts imports the generated modules", () => {
    const offenders: string[] = [];
    for (const file of productionFiles()) {
      if (file === CLIENT) continue;
      visit(parse(file), (node) => {
        if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier) && node.moduleSpecifier.text.includes("wailsjs")) {
          offenders.push(`${rel(file)} imports ${node.moduleSpecifier.text}`);
        }
      });
    }
    expect(offenders).toEqual([]);
  });

  it("no production file accesses a property named go or runtime — on anything", () => {
    // Keyed on the PROPERTY, not on `window`. A drill found that a rule keyed on `window` misses
    // `(window as unknown as Record<string, unknown>)["go"]`, and an alias (`const w = window; w.go`)
    // would defeat any such rule. Nothing in this application has a legitimate `.go` or `.runtime`,
    // so forbidding the property outright closes every spelling at once.
    const offenders: string[] = [];
    for (const file of productionFiles()) {
      const source = parse(file);
      visit(source, (node) => {
        const at = () => `${rel(file)}:${source.getLineAndCharacterOfPosition(node.getStart()).line + 1}`;
        if (ts.isPropertyAccessExpression(node) && /^(go|runtime)$/.test(node.name.text)) {
          offenders.push(`${at()} .${node.name.text}`);
        }
        if (
          ts.isElementAccessExpression(node) &&
          (ts.isStringLiteral(node.argumentExpression) || ts.isNoSubstitutionTemplateLiteral(node.argumentExpression)) &&
          /^(go|runtime)$/.test(node.argumentExpression.text)
        ) {
          offenders.push(`${at()} ['${node.argumentExpression.text}']`);
        }
      });
    }
    expect(offenders).toEqual([]);
  });

  it("the client imports every generated façade and references every generated function", () => {
    const client = readFileSync(CLIENT, "utf8");
    const missing: string[] = [];
    for (const module of generatedModules()) {
      if (!new RegExp(`import \\* as ${module} from "[^"]*wailsjs/go/api/${module}"`).test(client)) {
        missing.push(`import of ${module}`);
      }
      for (const fn of generatedFunctions(module)) {
        if (!new RegExp(`\\b${module}\\.${fn}\\b`).test(client)) missing.push(`${module}.${fn}`);
      }
    }
    expect(missing).toEqual([]);
  });

  it("the generated files publish under window.go.api — the Go package the bindings live in", () => {
    for (const module of generatedModules()) {
      const js = readFileSync(join(GENERATED, `${module}.js`), "utf8");
      const namespaces = new Set([...js.matchAll(/window\['go'\]\['([^']+)'\]/g)].map((m) => m[1]));
      expect([...namespaces], `${module}.js`).toEqual(["api"]);
    }
  });
});

describe("G3 — every client function has a caller outside the api layer", () => {
  it("reads the client's shape (a gate with no input checks nothing)", () => {
    expect(clientShape().length).toBeGreaterThanOrEqual(4);
  });

  it("every client function is called from the application", () => {
    const callers = productionFiles().filter((f) => !f.includes(join("src", "api")));
    const referenced = new Set<string>();
    for (const file of callers) for (const ref of referencesIn(file)) referenced.add(ref);

    const unreached = clientShape().filter((fn) => !referenced.has(fn));
    expect(
      unreached,
      "client functions nothing in src/app, src/screens, src/i18n or src/ui calls — a binding reachable from " +
        "Go and the client but from no screen is Mizan's 10.18 defect (the FX engine, unbound for nine phases)",
    ).toEqual([]);
  });
});

describe("G5 (source) — test helpers never reach production code", () => {
  it("no production file imports the fake client", () => {
    const offenders: string[] = [];
    for (const file of productionFiles()) {
      visit(parse(file), (node) => {
        if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier) && /(^|\/)testing$/.test(node.moduleSpecifier.text)) {
          offenders.push(rel(file));
        }
      });
    }
    expect(offenders).toEqual([]);
  });
});

describe("the real client over the generated bindings", () => {
  const scope = globalThis as unknown as { go?: unknown };
  afterEach(() => {
    delete scope.go;
  });

  it("with no Wails runtime, fails as bridge-unavailable — never with fake data", async () => {
    await expect(createClient().app.health()).rejects.toMatchObject({ code: CODE_BRIDGE_UNAVAILABLE });
  });

  it("with a bridge under window.go.api, reaches it through the generated functions", async () => {
    // Installed as the Wails runtime installs it. This is the end-to-end seam Mizan's 10.17 lacked: the
    // generated function, the namespace, and the client's unwrap, exercised together.
    const received: unknown[] = [];
    scope.go = {
      api: {
        App: {
          Health: async () => ({ ok: true, data: { version: "9", schemaVersion: 1, platform: "windows", dataDir: "C:\\d" } }),
          BootStatus: async () => ({ ok: true, data: { state: "ready", phase: "done", current: 0, total: 0 } }),
        },
        Settings: {
          Get: async () => ({ ok: true, data: { locale: "ar", direction: "rtl" } }),
          Update: async (input: unknown) => {
            received.push(input);
            return { ok: true, data: { locale: "en", direction: "ltr" } };
          },
        },
      },
    };
    const client = createClient();
    await expect(client.app.health()).resolves.toMatchObject({ platform: "windows" });
    await expect(client.settings.update({ locale: "en" })).resolves.toEqual({ locale: "en", direction: "ltr" });
    expect(received).toEqual([expect.objectContaining({ locale: "en" })]);
  });
});
