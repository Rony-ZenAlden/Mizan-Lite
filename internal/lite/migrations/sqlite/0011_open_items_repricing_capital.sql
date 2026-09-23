-- 0011_open_items_repricing_capital.sql — what the owner approved on 2026-09-23, after using 0.9.8.
--
--   §1 An open-priced item (متفرقات): sold at a price typed at the till, never counted in stock.
--   §2 The rate a price was set at, so a price the dollar has left behind can be found — and a re-price PROPOSED to
--      the owner, never applied without them.
--   §3 A daily snapshot of what the shop is worth in dollars, so a pound losing value is visible over time.
--
-- What is NOT here: the built-in "Miscellaneous" item itself. A row seeded by SQL would sit in every catalogue from the
-- moment of upgrade, including shops that never press the button, and its name key would have to be copied by hand from
-- what Go computes. The till creates it through the catalogue the first time it is asked for (sales.OpenItem).
--
-- Plain columns and one new table: nothing here rebuilds a table, so nothing here has 0010's ordering hazard.
-- Every nullable comparison in a CHECK is guarded (PROGRESS O7).

-- ── §1. The open-priced item ────────────────────────────────────────────────────────────────────────────────────────
--
-- A pantry shop sells things all day that have no barcode and no card in the system: a carrier bag, a bunch of parsley,
-- one lemon. Without this, each is either defined as a product first — which nobody does with a customer waiting — or
-- sold outside the application, where it is missing from the takings and the drawer count.
--
-- An open-priced product carries no price of its own (0) and never holds stock: the price is typed at the till, and the
-- sale moves money without moving any stock.
ALTER TABLE products ADD COLUMN open_price SMALLINT NOT NULL DEFAULT 0 CHECK (open_price IN (0, 1));

-- The sale line keeps its own copy, as it keeps the name and the unit: a line is read by the reports as what it was
-- when it was sold, and a report must not change because a product was later edited.
ALTER TABLE sale_lines ADD COLUMN open_price SMALLINT NOT NULL DEFAULT 0 CHECK (open_price IN (0, 1));

-- Whether a product is open-priced is decided when it is created and never changes. A stocked product turned open would
-- strand its stock outside every count; an open product turned stocked would start from a history of sales that never
-- took anything off a shelf.
CREATE TRIGGER tr_products_open_price_fixed BEFORE UPDATE OF open_price ON products
WHEN NEW.open_price <> OLD.open_price
BEGIN
  SELECT RAISE(ABORT, 'whether a product is open-priced is fixed when it is created');
END;

-- An open-priced product holds no stock, so no movement may ever name one. The stock service refuses first, with a
-- message a person can read; this is what holds if a later change forgets to.
CREATE TRIGGER tr_stock_ledger_no_open_price BEFORE INSERT ON stock_ledger
WHEN (SELECT open_price FROM products WHERE id = NEW.product_id) = 1
BEGIN
  SELECT RAISE(ABORT, 'an open-priced product holds no stock');
END;

-- Nor a reorder level: "tell me when it runs low" means nothing for something that is never counted.
CREATE TRIGGER tr_products_open_price_no_reorder_insert BEFORE INSERT ON products
WHEN NEW.open_price = 1 AND NEW.reorder_micro IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'an open-priced product has no reorder level');
END;
CREATE TRIGGER tr_products_open_price_no_reorder_update BEFORE UPDATE ON products
WHEN NEW.open_price = 1 AND NEW.reorder_micro IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'an open-priced product has no reorder level');
END;

-- ── §2. The rate a price was set at ─────────────────────────────────────────────────────────────────────────────────
--
-- A product priced in pounds two months ago, with the dollar up 15% since, is being sold below what it now costs to
-- replace. To find it, the product needs to remember the exchange rate that was in force when its price was set.
--
-- A SNAPSHOT, not a timestamp. The rate in force is resolved by fx_rates' highest seq and never by clock (L3), so
-- "when was it priced" looked up against recorded_at would reintroduce the clock this design keeps out. The catalogue
-- writes the rate in force at every price change from now on.
--
-- NULL where no rate was in force when the price was set — and on every open-priced product, whose price is typed at
-- the till and cannot go stale.
ALTER TABLE products ADD COLUMN priced_rate_nano BIGINT CHECK (priced_rate_nano IS NULL OR priced_rate_nano > 0);

-- Products that exist before this migration were priced at an unknown moment. The best available estimate is the rate
-- of the business day they were last changed — the rule the reports already use for an event with no rate of its own
-- (L6: fx_rates' highest seq with business_date on or before the day). updated_at moves on ANY edit, so this reads a
-- product as priced no earlier than it really was: it can under-report how stale a price is, never over-report it.
-- A product last changed before the first rate takes the first rate.
UPDATE products SET priced_rate_nano = COALESCE(
  (SELECT r.local_per_usd_nano FROM fx_rates r
     WHERE r.business_date <= substr(products.updated_at, 1, 10)
     ORDER BY r.seq DESC LIMIT 1),
  (SELECT r.local_per_usd_nano FROM fx_rates r ORDER BY r.seq ASC LIMIT 1))
WHERE open_price = 0;

-- ── §3. What the shop is worth in dollars, day by day ───────────────────────────────────────────────────────────────
--
-- A profit in pounds can be a loss in anything that holds its value. The question an owner in this economy needs
-- answered is whether the shop is bigger than it was a month ago MEASURED IN DOLLARS — and how much of the pounds it
-- held lost value to the exchange rate while it held them.
--
-- One row per business day, rewritten through the day and final once the day is over. Each part is kept in its own
-- currency with the rate beside it, so the dollar total is always derivable and never a second figure free to
-- disagree with the ones it came from (DESIGN D9's reasoning).
CREATE TABLE capital_snapshots (
  business_date      CHAR(10) NOT NULL PRIMARY KEY,
  taken_at           CHAR(24) NOT NULL,
  local_currency     CHAR(3)  NOT NULL REFERENCES currencies(code),
  local_per_usd_nano BIGINT   NOT NULL CHECK (local_per_usd_nano > 0),
  -- Stock at average cost, which the ledger holds in dollars. May be below zero where a shop sold beyond its stock.
  stock_usd_minor    BIGINT   NOT NULL,
  -- What the drawer is expected to hold in each currency; below zero if the owner took out more than it held.
  cash_usd_minor     BIGINT   NOT NULL,
  cash_local_minor   BIGINT   NOT NULL,
  -- What customers owe the shop, net, in each currency; a customer in credit reduces it.
  owed_usd_minor     BIGINT   NOT NULL,
  owed_local_minor   BIGINT   NOT NULL
);
