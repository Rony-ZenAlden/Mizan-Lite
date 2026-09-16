import { existsSync, mkdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { LOCALES, call, checkStructure, enterPin, expectReadable, go, label, readable, reset, setFiles, state, type Locale } from "./lite";

// L8 §3.4: each journey drives the real screens as a person would — keyboard and clicks, never Go directly — and ends by asserting
// what Go recorded, through the bindings. Every journey runs in Arabic and in English.

const MOLASSES = { barcode: "6290001000035", ar: "دبس رمان", en: "Pomegranate molasses" }; // a jar, 45,000 SYP
const name = (p: { ar: string; en: string }, locale: Locale) => p[locale];
const receiptDialog = (page: Page) => page.getByRole("dialog").filter({ has: page.getByTestId("receipt") });

async function scan(page: Page, locale: Locale, code: string) {
  const field = page.getByLabel(label(locale, "till.scan"));
  await field.fill(code);
  await field.press("Enter");
}

for (const locale of LOCALES) {
  test.describe(locale, () => {
    test(`J1 first run: shop, language, PIN twice, the recovery code, the rate — ${locale}`, async ({ page }) => {
      await reset(page, "empty", locale);
      await expect(page.getByRole("heading", { name: label("ar", "firstrun.title") })).toBeVisible();
      if (locale === "en") {
        await page.getByRole("button", { name: "English", exact: true }).click();
        await expect(page.getByRole("heading", { name: label("en", "firstrun.title") })).toBeVisible();
      }
      await page.getByLabel(label(locale, "firstrun.shop_name")).fill("بقالية الاختبار");
      await page.getByLabel(label(locale, "firstrun.pin"), { exact: true }).fill("246813");
      await page.getByLabel(label(locale, "firstrun.pin_confirm")).fill("246813");
      await page.getByLabel(label(locale, "firstrun.rate", { currency: label(locale, "currency.SYP") })).fill("15000");
      await expectReadable(page.getByTestId("rate-readback"), "15,000");
      await checkStructure(page, locale, "first run");
      await page.getByRole("button", { name: label(locale, "firstrun.submit") }).click();

      const code = await page.getByTestId("recovery-code").textContent();
      expect(code).toMatch(/^[A-Z0-9]{4}(-[A-Z0-9]{4}){3}$/);
      await page.getByLabel(label(locale, "recovery.confirm")).check();
      await page.getByRole("button", { name: label(locale, "recovery.finish") }).click();

      await expect(page.getByLabel(label(locale, "till.scan"))).toBeFocused();
      await checkStructure(page, locale, "the till of a new shop");
      expect(await call(page.request, "Settings", "Get")).toMatchObject({ shopName: "بقالية الاختبار", locale });
      const rate = await call<{ set: boolean; rate: string }>(page.request, "FX", "Current");
      expect(rate.set).toBe(true);
    });

    test(`J2 products from Excel and a delivery — ${locale}`, async ({ page }) => {
      const { saveDir } = await reset(page, "seeded", locale);
      const before = (await call<unknown[]>(page.request, "Catalog", "Products", { text: "", includeInactive: true })).length;
      await go(page, locale, "nav.products");
      await page.getByRole("button", { name: label(locale, "import.open") }).click();
      const dialog = page.getByRole("dialog", { name: label(locale, "import.title") });
      await dialog.getByRole("button", { name: label(locale, "import.template") }).click();
      await expect(dialog.getByRole("status")).toBeVisible();
      const template = (await state(page)).saved.at(-1)!;
      expect(template.startsWith(saveDir) && existsSync(template)).toBeTruthy();
      await setFiles(page, { open: template });
      await dialog.getByRole("button", { name: label(locale, "import.choose") }).click();
      await expectReadable(dialog.getByRole("status").last(), readable(label(locale, "import.ready", { count: "2", stock: "1" })));
      await checkStructure(page, locale, "import preview");
      await dialog.getByRole("button", { name: label(locale, "import.apply", { count: "2" }) }).click();
      await expect(dialog).toBeHidden(); // no PIN since 2026-09-16
      const products = await call<{ nameAr: string; id: string }[]>(page.request, "Catalog", "Products", { text: "", includeInactive: true });
      expect(products.length).toBe(before + 2);

      await go(page, locale, "nav.stock");
      const oil = products.find((p) => p.nameAr === "زيت زيتون (مثال)")!;
      const row = page.getByRole("row").filter({ hasText: locale === "ar" ? "زيت زيتون (مثال)" : "Olive oil (example)" }).first();
      await row.getByRole("button", { name: label(locale, "stock.receive") }).click();
      const quantity = label(locale, "stock.quantity", { unit: label(locale, "uom.l") });
      const receive = page.getByRole("dialog").filter({ has: page.getByLabel(quantity) });
      await receive.getByLabel(quantity).fill("5");
      await receive.getByLabel(label(locale, "stock.cost_mode")).selectOption("unit");
      await receive.getByLabel(label(locale, "stock.cost_unit")).fill("2.50");
      await checkStructure(page, locale, "receive dialog");
      await receive.getByRole("button", { name: label(locale, "action.save") }).click();
      await expect(receive).toBeHidden();
      const levels = await call<{ productId: string; onHand: string }[]>(page.request, "Stock", "Levels");
      expect(levels.find((l) => l.productId === oil.id)?.onHand).toBe("25.000");
    });

    test(`J3 a cash sale by keyboard only, printed, then a copy — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      const before = await call<{ sales: unknown[] }>(page.request, "Sales", "List", "");
      await expect(page.getByLabel(label(locale, "till.scan"))).toBeFocused();
      await scan(page, locale, MOLASSES.barcode);
      await expect(page.getByTestId("cart-line")).toHaveCount(1);
      await page.keyboard.press("+");
      await expect(page.getByLabel(label(locale, "till.quantity_of", { name: name(MOLASSES, locale) }))).toHaveValue("2");
      await checkStructure(page, locale, "till with a cart");
      await page.keyboard.press("F9");

      const receipt = receiptDialog(page);
      await expect(receipt).toBeVisible();
      await expect(receipt.getByTestId("receipt")).toContainText("90,000");
      await checkStructure(page, locale, "receipt");
      await receipt.getByRole("button", { name: label(locale, "print.receipt") }).click();
      await expect(receipt.getByRole("status")).toBeVisible();
      await receipt.getByRole("button", { name: label(locale, "print.copy") }).click();
      await expectReadable(receipt.getByRole("status"), readable(label(locale, "print.sent_copy", { printer: "Demo XP-80 (طابعة تجريبية)", number: "2" })));
      await receipt.getByRole("button", { name: label(locale, "action.close") }).click();
      await expect(page.getByLabel(label(locale, "till.scan"))).toBeFocused();

      const after = await call<{ sales: { total: string }[] }>(page.request, "Sales", "List", "");
      expect(after.sales.length).toBe(before.sales.length + 1);
      expect(after.sales.at(-1)!.total).toBe("90000");
      expect((await state(page)).printed).toBe(2);
    });

    test(`J4 a credit sale prints itself; a repayment's voucher follows — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      await scan(page, locale, MOLASSES.barcode);
      await expect(page.getByTestId("cart-line")).toHaveCount(1);
      await page.keyboard.press("F4");
      const picker = page.getByRole("dialog", { name: label(locale, "customers.pick_title") });
      await picker.getByRole("button", { name: /سمير الحداد/ }).click();
      await expect(picker).toBeHidden();
      // exact: the payment-details toggle also carries the word "Pay" in English (owner's UX changes, 2026-09-16)
      await page.getByRole("button", { name: label(locale, "till.pay"), exact: true }).click();
      const receipt = receiptDialog(page);
      await expect(receipt.getByTestId("receipt-credit")).toBeVisible();
      await expect(receipt.getByRole("status")).toBeVisible(); // printed by itself (Q-L7.2)
      await receipt.getByRole("button", { name: label(locale, "action.close") }).click();

      await go(page, locale, "nav.customers");
      await page.getByRole("row").filter({ hasText: "سمير الحداد" }).getByRole("button", { name: label(locale, "customers.statement") }).click();
      const statement = page.getByRole("dialog").filter({ has: page.getByTestId("statement-balance") });
      await statement.getByRole("button", { name: label(locale, "statement.take_payment") }).click();
      const payment = page.getByRole("dialog").filter({ has: page.getByLabel(label(locale, "payment.all")) });
      await payment.getByLabel(label(locale, "payment.all")).check();
      await expect(payment.getByTestId("payment-quote")).toBeVisible();
      await checkStructure(page, locale, "payment dialog");
      await payment.getByRole("button", { name: label(locale, "payment.record") }).click();
      const voucher = page.getByRole("dialog").filter({ has: page.getByTestId("print-panel") }).last();
      await expect(voucher.getByRole("status")).toBeVisible();
      await expect(voucher.getByTestId("print-preview")).toBeVisible();

      expect((await state(page)).printed).toBe(2);
      const customers = await call<{ id: string; name: string }[]>(page.request, "Customers", "Search", { text: "سمير", owingOnly: false, includeInactive: false });
      const st = await call<{ entries: { kind: string }[]; balance: string }>(page.request, "Customers", "Statement", { customerId: customers[0]!.id, currency: "SYP" });
      expect(st.entries.at(-1)?.kind === "payment" || st.balance === "0").toBeTruthy();
    });

    test(`J5 a void states the cash to hand back before it is confirmed — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      const day = await call<{ sales: { id: string; receiptNo: number; status: string }[] }>(page.request, "Sales", "List", "");
      const target = day.sales.filter((s) => s.status === "posted").at(-1)!;
      await go(page, locale, "nav.sales");
      await page.getByRole("row").filter({ has: page.getByRole("cell", { name: String(target.receiptNo), exact: true }) }).getByRole("button", { name: label(locale, "sales.open") }).click();
      const receipt = receiptDialog(page);
      await receipt.getByRole("button", { name: label(locale, "receipt.void") }).click();
      await expect(receipt.getByTestId("void-hand-back")).toBeVisible();
      await receipt.getByLabel(label(locale, "receipt.void_reason")).fill("e2e");
      await checkStructure(page, locale, "void form");
      await receipt.getByRole("button", { name: label(locale, "receipt.void_confirm") }).click(); // no PIN since 2026-09-16
      await expect(receipt.getByTestId("receipt")).toContainText(readable(label(locale, "receipt.voided", { reason: "" })).replace(/[—\s-]+$/, ""));
      const after = await call<{ sales: { id: string; status: string }[] }>(page.request, "Sales", "List", "");
      expect(after.sales.find((s) => s.id === target.id)?.status).toBe("voided");
    });

    test(`J6 the drawer: a count and an expense, both at the counter — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      await go(page, locale, "nav.cash");
      await page.getByTestId("drawer-SYP").getByRole("button", { name: label(locale, "cash.count.action") }).click();
      const count = page.getByRole("dialog", { name: label(locale, "cash.count.title", { currency: label(locale, "currency.SYP") }) });
      await count.getByLabel(label(locale, "cash.counted")).fill("1000000");
      await count.getByRole("button", { name: label(locale, "cash.count.save") }).click();
      await expect(count).toBeHidden();
      await page.getByRole("button", { name: label(locale, "cash.expense.action") }).click();
      const expense = page.getByRole("dialog", { name: label(locale, "cash.expense.title") });
      await expense.getByLabel(label(locale, "cash.amount")).fill("25000");
      await expense.getByLabel(label(locale, "cash.category")).selectOption("electricity");
      await checkStructure(page, locale, "expense dialog");
      await expense.getByRole("button", { name: label(locale, "action.save") }).click();
      await expect(expense).toBeHidden(); // no PIN since 2026-09-16
      const drawer = await call<{ entries: { kind: string }[] }>(page.request, "Cash", "Drawer", "");
      expect(drawer.entries.map((e) => e.kind)).toEqual(expect.arrayContaining(["count", "expense"]));
    });

    test(`J7 every report opens at the counter, with no PIN — ${locale}`, async ({ page }) => {
      // Until 2026-09-16 this journey entered the PIN and then watched the figures disappear when owner mode ended. The
      // owner asked for the shop's own figures to be readable at the counter; what it proves now is that they simply are.
      await reset(page, "seeded", locale);
      await go(page, locale, "nav.reports");
      for (const tab of ["day", "month", "products", "stock"]) {
        await page.getByRole("tab", { name: label(locale, `reports.tab.${tab}`) }).click();
        await expect(page.getByRole("button", { name: label(locale, "export.xlsx") })).toBeVisible();
        await checkStructure(page, locale, `reports ${tab}`);
      }
      // No PIN was ever asked for, and the profit figures are on screen.
      await expect(page.getByRole("dialog", { name: label(locale, "pin.title") })).toBeHidden();
      await expect(page.getByRole("button", { name: label(locale, "reports.show") })).toBeHidden();
      const status = await call<{ elevatedSeconds: number }>(page.request, "Owner", "Status");
      expect(status.elevatedSeconds).toBe(0);
    });

    test(`J8 exports: every report and the ledger, to Excel and PDF — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      await go(page, locale, "nav.reports"); // no PIN since 2026-09-16
      for (const tab of ["day", "month", "products", "stock"]) {
        await page.getByRole("tab", { name: label(locale, `reports.tab.${tab}`) }).click();
        for (const format of ["xlsx", "pdf"]) {
          await page.getByRole("button", { name: label(locale, `export.${format}`) }).click();
          await expect(page.getByTestId("export").getByRole("status")).toBeVisible();
        }
      }
      await go(page, locale, "nav.customers");
      const ledger = page.getByRole("region", { name: label(locale, "customers.export_ledger") });
      await ledger.getByRole("button", { name: label(locale, "export.pdf") }).click();
      await expect(ledger.getByRole("status")).toBeVisible();
      const saved = (await state(page)).saved;
      expect(saved.length).toBe(9);
      for (const file of saved) expect(statSync(file).size, file).toBeGreaterThan(1000);
      expect(saved.filter((f) => f.endsWith(".xlsx"))).toHaveLength(4);
    });

    test(`J9 backups: now, an outside folder, a restore and its restart — ${locale}`, async ({ page }) => {
      const { saveDir } = await reset(page, "seeded", locale);
      await go(page, locale, "nav.backups");
      await page.getByRole("button", { name: label(locale, "backups.take_now") }).click();
      await expect(page.getByTestId("backup-row")).not.toHaveCount(0);
      const outside = join(saveDir, "usb");
      mkdirSync(outside, { recursive: true });
      await setFiles(page, { folder: outside });
      await page.getByRole("button", { name: label(locale, "backups.outside_change") }).click(); // no PIN since 2026-09-16
      await expect(page.getByText(outside)).toBeVisible();
      await checkStructure(page, locale, "backups");

      const sold = await call<{ sales: unknown[] }>(page.request, "Sales", "List", "");
      await page.getByTestId("backup-row").first().getByRole("button", { name: label(locale, "backups.restore") }).click();
      const restore = page.getByRole("dialog", { name: label(locale, "restore.title") });
      await expect(restore.getByTestId("restore-loss")).toBeVisible();
      await checkStructure(page, locale, "restore dialog");
      await restore.getByRole("button", { name: label(locale, "restore.confirm") }).click();
      // A restore is one of the two acts that still ask (owner.ReservedActs): it replaces the shop's books wholesale.
      await enterPin(page, locale);
      await expect(restore.getByRole("status")).toBeVisible();
      await expect.poll(async () => (await call<{ restored?: unknown }>(page.request, "Backups", "Status")).restored, { timeout: 20_000 }).toBeTruthy();
      await page.goto("/#/about");
      await expect(page.getByText(label(locale, "backups.restored"))).toBeVisible();
      const after = await call<{ sales: unknown[] }>(page.request, "Sales", "List", "");
      expect(after.sales.length).toBe(sold.sales.length);
    });

    test(`J10 the rate by hand, and the language switched mid-session — ${locale}`, async ({ page }) => {
      await reset(page, "seeded", locale);
      await go(page, locale, "nav.rates");
      await page.getByLabel(label(locale, "rates.rate_label", { currency: label(locale, "currency.SYP") })).fill("15300");
      await checkStructure(page, locale, "rates");
      await page.getByRole("button", { name: label(locale, "rates.save") }).click(); // no PIN since 2026-09-16
      await expect.poll(async () => (await call<{ rate: string }>(page.request, "FX", "Current")).rate).toMatch(/^15,?300/);

      const other: Locale = locale === "ar" ? "en" : "ar";
      await page.getByRole("button", { name: label(other, `language.${other}`), exact: true }).click();
      await expect(page.locator("html")).toHaveAttribute("dir", other === "ar" ? "rtl" : "ltr");
      await expect(page.getByRole("navigation", { name: label(other, "nav.label") })).toBeVisible();
      await checkStructure(page, other, "rates after switching language");
      expect((await call<{ locale: string }>(page.request, "Settings", "Get")).locale).toBe(other);
    });
  });
}
