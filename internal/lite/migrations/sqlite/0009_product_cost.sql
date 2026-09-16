-- 0009_product_cost.sql — the price a shop paid, and the margin it sells at.
--
-- The owner asked for this on 2026-09-16, after using 0.9.1 in a seeded shop: an explicit cost price on the product, so the
-- sell price can be worked out from a margin, and so profit reflects the price the shop believes it paid (the owner's
-- answer the same day: the typed cost is the profit basis).
--
-- The stocktake that was asked for alongside it needs tables of its own; its schema is drafted in
-- docs/mizan_lite/phases/L9_AUDIT_DRAFT.sql and ships with the module that uses it, not before.
--
-- Every nullable comparison in a CHECK is guarded (PROGRESS O7).

-- ── The price a shop paid ───────────────────────────────────────────────────────────────────────────────────────────────
--
-- Nullable on purpose: a shop that has never typed one is not lying about its costs, and the column must not claim zero.
-- Where it is NULL the weighted average from the deliveries (stock_levels.avg_cost_usd_micro) is what a sale snapshots, as it
-- always did. Where it is set, it is what a sale snapshots from now on.
--
-- The MARGIN is not stored. It is sell_price − cost_price, as a percentage or an amount, and storing it would be a second
-- copy of a figure that is already there — free to disagree with the two it came from (DESIGN D9's reasoning, applied to a
-- derived number).
ALTER TABLE products ADD COLUMN cost_price_micro BIGINT;
ALTER TABLE products ADD COLUMN cost_currency CHAR(3) REFERENCES currencies(code);

-- Both together or neither: an amount without its currency is not a price.
CREATE TRIGGER tr_products_cost_pair_insert BEFORE INSERT ON products
WHEN (NEW.cost_price_micro IS NULL) <> (NEW.cost_currency IS NULL)
BEGIN
  SELECT RAISE(ABORT, 'a cost price needs both an amount and a currency');
END;
CREATE TRIGGER tr_products_cost_pair_update BEFORE UPDATE ON products
WHEN (NEW.cost_price_micro IS NULL) <> (NEW.cost_currency IS NULL)
BEGIN
  SELECT RAISE(ABORT, 'a cost price needs both an amount and a currency');
END;
-- A cost of nothing is a typing mistake, not a free product.
CREATE TRIGGER tr_products_cost_positive_insert BEFORE INSERT ON products
WHEN NEW.cost_price_micro IS NOT NULL AND NEW.cost_price_micro <= 0
BEGIN
  SELECT RAISE(ABORT, 'a cost price is more than nothing');
END;
CREATE TRIGGER tr_products_cost_positive_update BEFORE UPDATE ON products
WHEN NEW.cost_price_micro IS NOT NULL AND NEW.cost_price_micro <= 0
BEGIN
  SELECT RAISE(ABORT, 'a cost price is more than nothing');
END;
