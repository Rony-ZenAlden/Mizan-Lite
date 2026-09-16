import { expect, test } from "@playwright/test";
import { LOCALES, call, capture, checkStructure, enterPin, go, label, reset, setFiles, type Locale } from "./lite";

// The visual pack (L8 D-L8.4): every screen and the main dialogs, in both languages, at the two window sizes a shop's computer
// has. Saved under build/lite-e2e/screens for a person to look through at every release; never compared pixel by pixel, because
// fonts differ per system and a golden that fails on a font update teaches people to regenerate goldens.
const SIZES = [
  { name: "1366x768", width: 1366, height: 768 },
  { name: "1024x768", width: 1024, height: 768 },
];

const SCREENS: { nav: string; route: string; owner?: boolean }[] = [
  { nav: "nav.till", route: "/" },
  { nav: "nav.sales", route: "/sales" },
  { nav: "nav.cash", route: "/cash" },
  { nav: "nav.customers", route: "/customers" },
  { nav: "nav.products", route: "/products" },
  { nav: "nav.stock", route: "/stock" },
  { nav: "nav.rates", route: "/rates" },
  { nav: "nav.reports", route: "/reports", owner: true },
  { nav: "nav.printer", route: "/printer" },
  { nav: "nav.backups", route: "/backups" },
  { nav: "nav.owner", route: "/owner", owner: true },
  { nav: "nav.about", route: "/about" },
];

for (const locale of LOCALES) {
  for (const size of SIZES) {
    test(`every screen, ${locale}, ${size.name}`, async ({ page }) => {
      await page.setViewportSize({ width: size.width, height: size.height });
      await reset(page, "seeded", locale);
      for (const screen of SCREENS) {
        await page.goto(`/#${screen.route}`);
        if (screen.owner) {
          const pin = page.getByRole("dialog", { name: label(locale, "pin.title") });
          if (await pin.isVisible().catch(() => false)) await enterPin(page, locale);
        }
        await expect(page.getByRole("navigation", { name: label(locale, "nav.label") })).toBeVisible();
        await page.waitForTimeout(250);
        await checkStructure(page, locale, `${screen.route} at ${size.name}`);
        await capture(page, `${size.name}/${locale}/${screen.nav.replace("nav.", "")}`);
      }
    });
  }
}

// The dialogs are where a shop spends its attention, and where text is most likely to overflow.
for (const locale of LOCALES) {
  test(`every main dialog, ${locale}`, async ({ page }) => {
    const shot = async (name: string) => {
      await page.waitForTimeout(200);
      await checkStructure(page, locale, name);
      await capture(page, `dialogs/${locale}/${name}`);
    };
    const { saveDir } = await reset(page, "seeded", locale);

    await page.getByLabel(label(locale, "till.scan")).fill("6290001000035");
    await page.getByLabel(label(locale, "till.scan")).press("Enter");
    await expect(page.getByTestId("cart-line")).toHaveCount(1);
    await page.keyboard.press("F9");
    const receipt = page.getByRole("dialog").filter({ has: page.getByTestId("receipt") });
    await expect(receipt).toBeVisible();
    await shot("receipt");
    await receipt.getByRole("tab", { name: label(locale, "print.view_paper") }).click();
    await expect(receipt.getByTestId("print-preview")).toBeVisible();
    await shot("receipt-as-printed");
    await receipt.getByRole("button", { name: label(locale, "receipt.void") }).click();
    await shot("void");
    await page.keyboard.press("Escape");

    await go(page, locale, "nav.customers");
    await page.getByRole("row").filter({ hasText: "أبو محمد - الحلاق" }).getByRole("button", { name: label(locale, "customers.statement") }).click();
    const statement = page.getByRole("dialog").filter({ has: page.getByTestId("statement-balance") });
    await expect(statement).toBeVisible();
    await shot("statement");
    await statement.getByRole("button", { name: label(locale, "statement.take_payment") }).click();
    await page.getByLabel(label(locale, "payment.all")).check();
    await expect(page.getByTestId("payment-quote")).toBeVisible();
    await shot("payment");
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");

    await go(page, locale, "nav.products");
    await page.getByRole("button", { name: label(locale, "import.open") }).click();
    const importDialog = page.getByRole("dialog", { name: label(locale, "import.title") });
    await importDialog.getByRole("button", { name: label(locale, "import.template") }).click();
    await expect(importDialog.getByRole("status")).toBeVisible();
    await setFiles(page, { open: (await call<{ path: string }>(page.request, "Catalog", "ImportTemplate")).path || `${saveDir}/x.xlsx` });
    await importDialog.getByRole("button", { name: label(locale, "import.choose") }).click();
    await expect(importDialog.getByTestId("import-preview")).toBeVisible();
    await shot("import");
    await page.keyboard.press("Escape");

    await go(page, locale, "nav.backups");
    await page.getByRole("button", { name: label(locale, "backups.take_now") }).click();
    await expect(page.getByTestId("backup-row").first()).toBeVisible();
    await page.getByTestId("backup-row").first().getByRole("button", { name: label(locale, "backups.restore") }).click();
    const pin = page.getByRole("dialog", { name: label(locale, "pin.title") });
    await shot("pin");
    await enterPin(page, locale);
    await expect(page.getByRole("dialog", { name: label(locale, "restore.title") })).toBeVisible();
    await shot("restore");
    await page.keyboard.press("Escape");
    expect(pin).toBeDefined();
  });
}
