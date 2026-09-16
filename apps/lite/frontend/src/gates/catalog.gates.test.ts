import ts from "typescript";
import { describe, expect, it } from "vitest";
import { CLIENT_CODES } from "@/api/envelope";
import { CATALOGS } from "@/i18n/messages";
import { parse, productionFiles, rel, visit } from "./source";

const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe("catalog parity", () => {
  for (const file of ["common", "errors"] as const) {
    it(`${file}: Arabic and English define exactly the same keys`, () => {
      const ar = Object.keys(CATALOGS.ar[file]).sort();
      const en = Object.keys(CATALOGS.en[file]).sort();
      expect(ar.length).toBeGreaterThan(10);
      expect(en).toEqual(ar);
    });

    it(`${file}: no translation is empty`, () => {
      for (const locale of ["ar", "en"] as const) {
        const empty = Object.entries(CATALOGS[locale][file]).filter(([, v]) => String(v).trim() === "");
        expect(empty, locale).toEqual([]);
      }
    });

    it(`${file}: each key has the same placeholders in both languages`, () => {
      // A translation that dropped {path} would hide where the shopkeeper's backup is.
      const ar = CATALOGS.ar[file] as Record<string, string>;
      const en = CATALOGS.en[file] as Record<string, string>;
      const mismatched = Object.keys(ar).filter((k) => placeholders(ar[k]!).join() !== placeholders(en[k] ?? "").join());
      expect(mismatched).toEqual([]);
    });
  }

  it("no key is defined in both files", () => {
    const both = Object.keys(CATALOGS.ar.common).filter((k) => k in CATALOGS.ar.errors);
    expect(both).toEqual([]);
  });

  it("the Arabic catalog is actually Arabic", () => {
    // Guards against the easiest parity mistake: English pasted into the Arabic file.
    const latinOnly = Object.entries(CATALOGS.ar.common as Record<string, string>).filter(
      // A template of placeholders and symbols only ("{usd} = {local}") has nothing to translate; one with Latin words does.
      ([key, value]) => !/[\u0600-\u06FF]/.test(value) && /[A-Za-z]/.test(value.replace(/\{\w+\}/g, "")) && key !== "language.en",
    );
    expect(latinOnly).toEqual([]);
  });

  it("every frontend error code has a translation in both languages", () => {
    for (const code of CLIENT_CODES) {
      expect(CATALOGS.ar.errors, code).toHaveProperty([code]);
      expect(CATALOGS.en.errors, code).toHaveProperty([code]);
    }
  });
});

describe("no hardcoded text", () => {
  const TEXT_ATTRIBUTES = new Set(["aria-label", "title", "placeholder", "alt", "label"]);
  const hasLetter = (s: string) => /\p{L}/u.test(s);

  it("no production component renders a literal string a person would read", () => {
    const offenders: string[] = [];
    for (const file of productionFiles().filter((f) => f.endsWith(".tsx"))) {
      const source = parse(file);
      visit(source, (node) => {
        const line = () => source.getLineAndCharacterOfPosition(node.getStart()).line + 1;
        if (ts.isJsxText(node) && hasLetter(node.text)) {
          offenders.push(`${rel(file)}:${line()} text "${node.text.trim()}"`);
        }
        if (ts.isJsxAttribute(node) && TEXT_ATTRIBUTES.has(node.name.getText()) && node.initializer && ts.isStringLiteral(node.initializer) && hasLetter(node.initializer.text)) {
          offenders.push(`${rel(file)}:${line()} ${node.name.getText()}="${node.initializer.text}"`);
        }
        if (ts.isJsxExpression(node) && node.expression && ts.isStringLiteral(node.expression) && hasLetter(node.expression.text) && ts.isJsxElement(node.parent)) {
          offenders.push(`${rel(file)}:${line()} {"${node.expression.text}"}`);
        }
      });
    }
    expect(offenders, "every string a person reads comes from the catalog").toEqual([]);
  });
});
