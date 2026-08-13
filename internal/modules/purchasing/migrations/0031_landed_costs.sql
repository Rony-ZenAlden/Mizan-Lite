-- 0031_landed_costs — freight, customs, and clearing, spread across what they brought in.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THESE ARE NOT JUST EXPENSES
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A business that books freight to an expense account understates what its stock cost and
-- overstates its margin on every sale of it. The goods on the shelf really did cost the invoice
-- plus the lorry plus the customs officer, and a gross margin computed without them is a number
-- that looks healthy and is not.
--
-- §D.3 lists this as trap 4 and names the mechanism: allocation across a receipt's lines by
-- value, weight, volume, or quantity, using largest-remainder so the total ties exactly.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THE BASIS IS DECLARED PER CHARGE
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Freight follows volume or weight — a lorry is full when it is full, whatever is in it.
-- Customs follows value, because that is what duty is charged on. Forcing one basis makes the
-- other wrong, and a shipment usually carries both kinds of charge at once.

CREATE TABLE landed_costs (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  -- The delivery these charges belong to. A charge with no delivery has nothing to spread
  -- across, which is why this is NOT NULL rather than a general-purpose expense row.
  receipt_id     CHAR(36)    NOT NULL REFERENCES goods_receipts(id) ON DELETE CASCADE,

  -- What kind of charge. Free text against a small vocabulary rather than a foreign key,
  -- because a customer's own categories ("port handling", "demurrage") must not need a release.
  charge_type    VARCHAR(40) NOT NULL,
  description    VARCHAR(400),

  -- Who is charging. A freight forwarder is not the supplier of the goods, and a landed cost
  -- billed to the wrong party is a payable nobody can reconcile. NULL where the cost is
  -- internal — a company's own lorry still costs something to run.
  partner_id     CHAR(36)    REFERENCES partners(id),

  -- How the charge spreads. `value` and `quantity` are computable from what a receipt already
  -- carries; `weight` and `volume` are named here so that adding them is a value change rather
  -- than a schema change (§D.4's pattern) — they need product dimensions the catalog does not
  -- yet hold, and the service refuses them rather than silently substituting another basis.
  basis          VARCHAR(20) NOT NULL DEFAULT 'value'
                 CHECK (basis IN ('value', 'quantity', 'weight', 'volume')),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  amount_minor   INTEGER     NOT NULL CHECK (amount_minor > 0),

  -- draft: entered, not yet applied. Nothing has moved.
  -- applied: TERMINAL. Spread across the lines, stock revalued, books written.
  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'applied', 'cancelled')),

  applied_at     VARCHAR(32),
  applied_by     CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL
);

CREATE INDEX ix_landed_costs_receipt ON landed_costs (receipt_id, status);
CREATE INDEX ix_landed_costs_partner ON landed_costs (partner_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- landed_cost_allocations — what each line actually absorbed
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Stored rather than recomputed, and the difference matters. The allocation depends on the
-- receipt's line values AT THE MOMENT IT WAS APPLIED; a later price correction (6.4) changes
-- those values, and recomputing would silently restate a charge that has already reached the
-- stock ledger and the books.
--
-- It is also the answer to "why is this item carried at that cost", which is the question a
-- margin nobody expected always ends in.

CREATE TABLE landed_cost_allocations (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  landed_cost_id CHAR(36)    NOT NULL REFERENCES landed_costs(id) ON DELETE CASCADE,
  receipt_line_id CHAR(36)   NOT NULL REFERENCES goods_receipt_lines(id),

  -- The weight this line carried in the split, in whatever the basis measures. Kept so the
  -- arithmetic can be re-read years later without reconstructing the receipt as it was.
  weight_micro   INTEGER     NOT NULL DEFAULT 0,
  amount_minor   INTEGER     NOT NULL DEFAULT 0,

  -- The revaluation this allocation wrote, where the goods were still on hand.
  movement_id    CHAR(36)    REFERENCES stock_movements(id),

  created_at     VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_landed_cost_allocations
  ON landed_cost_allocations (landed_cost_id, receipt_line_id);
CREATE INDEX ix_landed_cost_allocations_line ON landed_cost_allocations (receipt_line_id);
