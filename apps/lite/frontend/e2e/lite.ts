import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, type APIRequestContext, type Locator, type Page } from "@playwright/test";

export type Locale = "ar" | "en";
export const LOCALES: Locale[] = ["ar", "en"];
export const SEED_PIN = "481537";

const catalog = (locale: Locale) => ({
  ...(JSON.parse(readFileSync(new URL(`../../../../internal/lite/locales/${locale}/common.json`, import.meta.url), "utf8")) as Record<string, string>),
  ...(JSON.parse(readFileSync(new URL(`../../../../internal/lite/locales/${locale}/errors.json`, import.meta.url), "utf8")) as Record<string, string>),
});
const CATALOGS = { ar: catalog("ar"), en: catalog("en") };

/** A catalog string, as the screen shows it — the journeys speak both languages from the same keys the screens use. */
export function label(locale: Locale, key: string, params: Record<string, string> = {}): string {
  const template = CATALOGS[locale][key];
  if (template === undefined) throw new Error(`no ${locale} catalog key ${key}`);
  // As the screen does (i18n/messages.inSentence): in Arabic every value in a sentence is isolated.
  const isolate = (value: string) =>
    locale !== "ar" || value === "" ? value : (/^\s*[-+−≈0-9]/.test(value) ? "\u2066" : "\u2068") + value + "\u2069";
  return template.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? isolate(params[name]!) : m));
}

/** What a reader sees: the isolates Arabic sentences carry removed. */
export const readable = (text: string | null) => (text ?? "").replace(/[⁦-⁩]/g, "").replace(/\s+/g, " ").trim();

/** Calls a binding the way the frontend does, and returns its data — for arranging a shop and asserting on Go's state. */
export async function call<T = unknown>(request: APIRequestContext, facade: string, method: string, ...args: unknown[]): Promise<T> {
  const response = await request.post("/call", { data: { s: facade, m: method, args } });
  expect(response.ok(), `${facade}.${method}`).toBeTruthy();
  const envelope = (await response.json()) as { ok: boolean; data: T; error?: { code: string } };
  if (!envelope.ok) throw new Error(`${facade}.${method}: ${envelope.error?.code}`);
  return envelope.data;
}

/** A new shop for the journey: "empty" (a fresh installation) or "seeded" (the demo shop), in the language asked for. */
export async function reset(page: Page, fixture: "empty" | "seeded", locale: Locale): Promise<{ saveDir: string; dataDir: string }> {
  const response = await page.request.post("/__e2e/reset", { data: { fixture } });
  expect(response.ok(), await response.text()).toBeTruthy();
  const info = (await response.json()) as { saveDir: string; dataDir: string };
  if (fixture === "seeded") await call(page.request, "Settings", "Update", { locale });
  await page.goto("/");
  return info;
}

export async function state(page: Page): Promise<{ saved: string[]; printed: number; dataDir: string }> {
  return (await (await page.request.get("/__e2e/state")).json()) as { saved: string[]; printed: number; dataDir: string };
}

export async function setFiles(page: Page, files: { open?: string; folder?: string; cancel?: boolean }) {
  await page.request.post("/__e2e/files", { data: files });
}

/** Answers the PIN dialog. */
export async function enterPin(page: Page, locale: Locale, pin = SEED_PIN) {
  const dialog = page.getByRole("dialog", { name: label(locale, "pin.title") });
  await dialog.getByLabel(label(locale, "pin.label")).fill(pin);
  await dialog.getByRole("button", { name: label(locale, "action.confirm") }).click();
  await expect(dialog).toBeHidden();
}

export async function go(page: Page, locale: Locale, navKey: string) {
  await page.getByRole("navigation", { name: label(locale, "nav.label") }).getByRole("link", { name: label(locale, navKey), exact: true }).click();
}

/**
 * The structural checks run on every view (L8 D-L8.4): no page that scrolls sideways, no text cut off by its own box, every
 * control named, no two navigation items alike, and — in Arabic — no control labelled in Latin letters only.
 */
export async function checkStructure(page: Page, locale: Locale, view: string) {
  const problems = await page.evaluate((arabic) => {
    const out: string[] = [];
    const doc = document.documentElement;
    if (doc.scrollWidth > doc.clientWidth + 1) out.push(`the page scrolls sideways (${doc.scrollWidth} > ${doc.clientWidth})`);
    const named = (el: Element) =>
      (el.getAttribute("aria-label") ?? "").trim() !== "" ||
      (el.textContent ?? "").trim() !== "" ||
      (el instanceof HTMLInputElement && (el.labels?.length ?? 0) > 0) ||
      (el instanceof HTMLSelectElement && (el.labels?.length ?? 0) > 0) ||
      (el instanceof HTMLTextAreaElement && (el.labels?.length ?? 0) > 0) ||
      el.getAttribute("aria-labelledby") !== null;
    for (const el of Array.from(document.querySelectorAll("button, a[href], input:not([type=hidden]), select, textarea"))) {
      const box = (el as HTMLElement).getBoundingClientRect();
      if (box.width === 0 && box.height === 0) continue;
      if (!named(el)) out.push(`an unnamed ${el.tagName.toLowerCase()}: ${el.outerHTML.slice(0, 80)}`);
    }
    for (const el of Array.from(document.querySelectorAll("button, label, th, h1, h2, h3, a, dt, legend"))) {
      const h = el as HTMLElement;
      const box = h.getBoundingClientRect();
      if (box.width === 0) continue;
      const style = getComputedStyle(h);
      if (style.overflow !== "visible" && h.scrollWidth > h.clientWidth + 2) out.push(`clipped text: "${(h.textContent ?? "").trim().slice(0, 40)}"`);
    }
    const nav = Array.from(document.querySelectorAll("nav a")).map((a) => (a.textContent ?? "").trim());
    const repeated = nav.filter((t, i) => nav.indexOf(t) !== i);
    if (repeated.length) out.push(`navigation labels repeated: ${repeated.join(", ")}`);
    if (arabic) {
      const allowed = /^(PDF|USB|ESC\/POS|F\d|×|—|\+|−|-|[\d\s.,:/%+≈=·-]*|English|.*[؀-ۿ].*)$/;
      for (const el of Array.from(document.querySelectorAll("button, nav a, label, th, h2, h3"))) {
        const text = (el.textContent ?? "").trim();
        if (/[A-Za-z]/.test(text) && !allowed.test(text) && !el.closest("[data-product-name], bdi")) out.push(`Latin-only in Arabic: "${text.slice(0, 40)}"`);
      }
    }
    return out;
  }, locale === "ar");
  expect(problems, `${view} (${locale})`).toEqual([]);
}

/** Saves the view into the visual pack (L8 §3.4) — looked at by a person each release, never compared pixel by pixel. */
export async function capture(page: Page, name: string) {
  await page.screenshot({ path: fileURLToPath(new URL(`../../../../build/lite-e2e/screens/${name}.png`, import.meta.url)), fullPage: false });
}

/** Waits until a locator's text, as a reader sees it, contains the expected text. */
export async function expectReadable(locator: Locator, text: string) {
  await expect.poll(async () => readable(await locator.textContent()), { message: text }).toContain(readable(text));
}
