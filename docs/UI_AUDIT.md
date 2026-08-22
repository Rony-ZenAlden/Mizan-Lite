# Mizan — front-end and workflow audit

A diagnostic of what exists today, written to be argued with. Every count is taken from the
source at commit `65ffcb8`, not from memory.

---

## 1. Full inventory

### 1.1 Routes — 30, all reachable from the sidebar

Routes are DATA (`frontend/src/app/shell/routes.tsx`), and the shell renders the menu and the
router from the same list, so a screen cannot be reachable without appearing in navigation.

| # | Path | Screen | Permission |
|---|------|--------|-----------|
| 1 | `/` | `DashboardScreen` | — (signed in) |
| 2 | `/needs-attention` | `NoticeCentre` | — |
| 3 | `/catalog` | `CatalogScreen` → `ProductDetail` | `catalog.view` |
| 4 | `/inventory/stock` | `StockScreen` → `MovementHistory` | `inventory.stock.view` |
| 5 | `/pos` | `POSTerminal` | `sales.document.draft` |
| 6 | `/sales/invoices` | `InvoicesScreen` → `InvoiceDetail` | `sales.document.view` |
| 7 | `/purchasing/orders` | `PurchaseOrdersScreen` → `PurchaseOrderDetail` | `purchasing.order.view` |
| 8 | `/purchasing/receipts` | `ReceiptsScreen` | `purchasing.order.view` |
| 9 | `/purchasing/bills` | `BillsScreen` | `purchasing.bill.view` |
| 10 | `/purchasing/returns` | `SupplierReturnsScreen` | `purchasing.order.view` |
| 11 | `/purchasing/payments` | `SupplierPaymentsScreen` | `purchasing.bill.view` |
| 12 | `/partners/customers` | `PartnersScreen role=customer` | `partner.customer.view` |
| 13 | `/partners/suppliers` | `PartnersScreen role=supplier` | `partner.supplier.view` |
| 14 | `/money/expenses` | `ExpensesScreen` | `expenses.expense.view` |
| 15 | `/money/debts` | `DebtsScreen` | `expenses.debt.view` |
| 16 | `/reports/statements` | `StatementsScreen` | `accounting.profit_and_loss.view` |
| 17 | `/reports/analysis` | `AnalysisScreen` | `sales.analysis.view` |
| 18 | `/reports/valuation` | `ValuationScreen` | `inventory.valuation.view` |
| 19 | `/accounting/chart` | `ChartScreen` | `accounting.account.view` |
| 20 | `/accounting/trial-balance` | `TrialBalanceScreen` | `accounting.account.view` |
| 21 | `/operations/backups` | `BackupScreen` | `identity.user.manage` |
| 22 | `/operations/import` | `ImportScreen` | `catalog.manage` |
| 23 | `/operations/system` | `SystemPanel` → `JobStatusPanel` | `identity.session.view` |
| 24 | `/admin/users` | `UsersScreen` | `identity.user.view` |
| 25 | `/admin/roles` | `RolesScreen` | `identity.role.view` |
| 26 | `/admin/sessions` | `SessionsScreen` | `identity.session.view` |
| 27 | `/admin/audit` | `AuditScreen` | `audit.entry.view` |
| 28 | `/account/password` | `ChangePasswordScreen` | — |
| 29 | `/help` | `HelpScreen` | — |
| 30 | *(setup)* | `SetupWizard` | pre-auth |

### 1.2 Components — 42 screens and panels

Non-routed components: `ProductDetail`, `NewProductForm`, `MovementHistory`, `CountDialog`,
`InvoiceDetail`, `PaymentPanel`, `ShiftBar`, `PurchaseOrderDetail`, `LandedCostsPanel`,
`PartnerDetail`, `PartnerStatement`, `SearchBox`, `JobStatusPanel`, `LoginScreen`, `SetupWizard`.

### 1.3 Dialogs — 4 only

`CreateUserDialog` (users), `CountDialog` (stock), the restore confirmation (backups), and
`Dialog` used inline by `RolesScreen`. **Everything else is inline or full-page.**

### 1.4 Shared UI library — 11 primitives

`Button` · `Input` · `Select` · `Checkbox`/`Switch` · `Dialog`/`Sheet` · `Tabs` · `Table` ·
`Badge` · `Tooltip` · `Toast` · `Alert`/`EmptyState`/`Skeleton`/`Spinner`

**46 colour tokens** in `src/index.css`, themed for light/dark and asserted for WCAG AA contrast
by `tokens.test.ts`.

---

## 2. Layout pattern — one shape, repeated 30 times

`AppShell` is `<aside>` sidebar + `<header>` + `<main>`. Every screen is then, almost without
exception:

```
<section className="flex flex-col gap-4|6">
  <header>  <h2>title</h2>  <p>help text</p>  </header>
  [<Alert> if error]
  [<Input> filter]
  <Table caption rowKey rows columns empty={<EmptyState/>} />
</section>
```

**24 of 30 screens are a heading, an optional filter, and one `<Table>`.** The four report
screens add a date range; the POS is the only genuinely different layout.

There are **no KPI cards** anywhere except `DashboardScreen`, which renders six `<article>` tiles
in a CSS grid — hand-rolled, not a shared component.

---

## 3. Current workflows, step by step

### Creating a product — *2 steps, added yesterday*
Catalogue → **New product** → type code + name → Save. Unit/category/type behind *More options*.
The form stays open and clears. Before this, **there was no form at all**.

### Executing a sale — *the best workflow in the product*
`/pos` → open shift (cash float) → scan/search → line added with price and tax → **Pay** →
method + amount → **Post** → print. Single screen, no page changes.

### Running a stock count — *2 steps, added yesterday*
Stock → **Count** on a row → type what is on the shelf → reason → Record. The delta is computed
server-side.

### Adding a user — *1 dialog*
Users → **Add user** → username, name, email, password, role → Create. Dialog closes, table
refreshes.

### Purchasing — **the worst workflow in the product**
Order → Deliveries → Bills → Payments are **four separate menu entries with no link between
them**. Nothing carries you from a placed order to receiving it. And:

> **`DraftReceipt`, `ReceiveLine`, `DraftBill`, `AddBillLine`, `Pay` and `CloseOrder` have Go
> bindings with no client function at all.** Deliveries, bills and payments are **read-only
> screens**. You can look at them; you cannot create one.

The only way to record a delivery today is the CSV importer or the database.

---

## 4. Bottlenecks

### 4.1 Why it reads as an admin dashboard

1. **One layout, 24 times.** Table-first, no hierarchy, no cards, no summary bands.
2. **Flat sidebar, 30 items, no grouping.** Odoo and QuickBooks group into 6–8 areas.
3. **No empty-state onboarding.** A fresh install shows "No products yet." with no next action.
4. **No inline creation.** Every write is a dialog or a separate screen; nothing edits in place.
5. **Density is uniform.** A 40-row stock list and a 3-field form get identical spacing.
6. **No micro-interaction.** No transitions, no optimistic updates, no skeletons on most screens.
7. **Typography is one size.** `text-sm`/`text-base` throughout; no display type, no numerals styling beyond `tabular-nums`.
8. **The dashboard is six tiles and nothing else** — no charts, no trend, no period comparison.

### 4.2 Functional gaps — ranked

| Severity | Gap |
|---|---|
| **Blocking** | 6 write bindings with no UI: `DraftReceipt`, `ReceiveLine`, `DraftBill`, `AddBillLine`, `Pay`, `CloseOrder`. **Purchasing cannot be operated.** |
| **High** | No product EDIT — create and read only. |
| **High** | No customer/supplier create form (`PartnersScreen` is read-only). |
| **High** | No expense create form (`ExpensesScreen` is read-only). |
| Medium | No category management UI. |
| Medium | No price-list UI; prices come from seeds or import. |
| Low | `SearchBox` is built and mounted nowhere. |

### 4.3 The reported user-creation bug — not reproducible

Tested at both layers:
- **Go:** create → list → assign role → **sign in** all pass.
- **React:** the dialog sends what was typed, and the list refreshes afterwards.

Both are now committed as regression tests. **If you can reproduce it, I need the exact message
and what you typed** — particularly the password, since under 12 characters is refused with a
validation error shown in the dialog, which is easy to read as "it did not work".

### 4.4 The pattern behind all of it

Four times now, in four layers: services built with nothing calling them (number series → no
creator; returns → no binding; 11 screens → no route; product creation → no form). **Section 4.2
is the fifth instance, found by this audit.**

Building and connecting are separate acts here, and nothing checks the connection.

---

## 5. What I would do, in order

1. **Purchasing write path** — 6 bindings, ~4 screens. Without it the module is a viewer.
2. **A structural test that every write binding has a caller** — kills the recurring pattern.
3. **Partner and expense create forms** — same shape as `NewProductForm`.
4. **Group the sidebar** into Sell / Buy / Stock / Money / Reports / Setup.
5. **Then** the visual redesign: a `Card`/`PageHeader`/`StatTile`/`Toolbar` layer, applied to the
   30 existing screens, which is mechanical once the components exist.

**1–3 are functionality; 4–5 are appearance.** A beautiful screen that cannot record a delivery is
worse than a plain one that can.
