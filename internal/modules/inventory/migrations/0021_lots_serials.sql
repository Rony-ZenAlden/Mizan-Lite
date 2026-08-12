-- 0021_lots_serials — batch and serial tracking (§21.2).
--
-- Owned by the inventory module, which owns 0020 already.
--
-- # From day one, behind flags
--
-- §21.2: "Lot/batch and serial tracking tables exist from day one, gated by feature flags —
-- pharmacy and electronics need them, a furniture store does not."
--
-- The tables and the columns exist for every install. What a furniture shop never sees is the
-- CONCEPT: no lot picker, no expiry column, no serial field. That distinction is the whole
-- design — shipping the schema costs three empty tables, and shipping it later costs a migration
-- that has to invent lots for stock that has already moved.
--
-- Phase 3 already put `tracking` on `products` with values none|quantity|lot|serial, so a
-- product declares which of these applies to it. This migration gives those values somewhere to
-- point.

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_lots — a batch, with the expiry that makes it matter
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stock_lots (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  variant_id    CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- The number printed on the box. Free text, deliberately: a lot number is a SUPPLIER's
  -- identifier, and every supplier formats theirs differently. Validating it would be refusing
  -- to receive a delivery because somebody else's convention is unexpected.
  lot_number    VARCHAR(80) NOT NULL,

  -- The two dates a pharmacy actually cares about. Both nullable: a batch of screws has a lot
  -- number for traceability and no expiry at all, and forcing a date would mean inventing one.
  expires_on    VARCHAR(10),
  manufactured_on VARCHAR(10),

  -- The supplier the batch came from, for a recall. NULL for stock that was found, counted in,
  -- or carried over from an opening balance.
  supplier_id   CHAR(36)    REFERENCES partners(id),

  -- Quarantined stock exists but must not be sold: a batch awaiting a quality check, or one
  -- under investigation after a complaint. Separate from is_active, which retires a lot that is
  -- finished with.
  is_quarantined INTEGER    NOT NULL DEFAULT 0 CHECK (is_quarantined IN (0, 1)),
  is_active     INTEGER     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL,

  CONSTRAINT ck_lot_dates CHECK (
    expires_on IS NULL OR manufactured_on IS NULL OR expires_on >= manufactured_on
  )
);

-- One lot number per variant. The same number against two variants is ordinary — a supplier's
-- batch covers several products — but the same number twice for ONE variant is a duplicate
-- nobody can tell apart at a recall.
CREATE UNIQUE INDEX ux_stock_lots_number ON stock_lots (variant_id, lot_number);

-- The FEFO order: first EXPIRED, first out. Indexed now because it is the order a pharmacy picks
-- in, and picking by receipt order instead is how a wholesaler ends up shipping the batch that
-- expires next week while holding one that expires next year.
CREATE INDEX ix_stock_lots_fefo
  ON stock_lots (variant_id, is_active, is_quarantined, expires_on);
CREATE INDEX ix_stock_lots_company ON stock_lots (company_id, expires_on);

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_lot_levels — how much of each lot is where
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A second projection, at a finer grain than stock_levels, and maintained the same way: the
-- movement ledger remains the source of truth and this can be rebuilt from it.
--
-- Separate from stock_levels rather than replacing it, because a lot-tracked product still needs
-- the coarse answer: "how many of these do we have" is asked far more often than "how many of
-- batch 47", and it must not become a sum over lots at every keystroke.

CREATE TABLE stock_lot_levels (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id  CHAR(36)    NOT NULL REFERENCES warehouses(id),
  variant_id    CHAR(36)    NOT NULL REFERENCES product_variants(id),
  lot_id        CHAR(36)    NOT NULL REFERENCES stock_lots(id),

  qty_on_hand_micro INTEGER NOT NULL DEFAULT 0,

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_stock_lot_levels ON stock_lot_levels (lot_id, warehouse_id);
CREATE INDEX ix_stock_lot_levels_variant ON stock_lot_levels (variant_id, warehouse_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_serials — one row per physical unit
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A serial is not a quantity, it is an IDENTITY. That difference drives everything here: a
-- serial has a current state and a current location rather than a count, and a movement of a
-- serialised product moves exactly one of them.

CREATE TABLE stock_serials (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  variant_id    CHAR(36)    NOT NULL REFERENCES product_variants(id),

  serial_number VARCHAR(80) NOT NULL,
  -- The lot it belongs to, where a business tracks both — a batch of phones with individual
  -- IMEIs. NULL when only serials are tracked.
  lot_id        CHAR(36)    REFERENCES stock_lots(id),

  -- Where it is now, and what state it is in. A serial that has been sold keeps its row: the
  -- warranty claim two years later needs to find it, and a deleted row cannot be found.
  warehouse_id  CHAR(36)    REFERENCES warehouses(id),
  status        VARCHAR(20) NOT NULL DEFAULT 'in_stock'
                CHECK (status IN ('in_stock', 'reserved', 'sold', 'returned',
                                  'scrapped', 'in_transit')),

  -- Who has it, once it has left. For a warranty, a recall, or a repair.
  partner_id    CHAR(36)    REFERENCES partners(id),

  warranty_until VARCHAR(10),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL
);

-- A serial number identifies exactly one physical thing, GLOBALLY within a company. Scoping it
-- per variant would let a warranty lookup return two devices — and a scan at a repair counter
-- has no way to choose between them.
CREATE UNIQUE INDEX ux_stock_serials_number ON stock_serials (company_id, serial_number);
CREATE INDEX ix_stock_serials_variant ON stock_serials (variant_id, status);
CREATE INDEX ix_stock_serials_warehouse ON stock_serials (warehouse_id, status);
CREATE INDEX ix_stock_serials_partner ON stock_serials (partner_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- the movement's link to a lot or a serial
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Added to stock_movements rather than kept in a side table, because a movement of a
-- lot-tracked product is not a movement PLUS a lot fact — it is one fact, and splitting it
-- would let the two halves be written apart.

ALTER TABLE stock_movements ADD COLUMN lot_id CHAR(36) REFERENCES stock_lots(id);
ALTER TABLE stock_movements ADD COLUMN serial_id CHAR(36) REFERENCES stock_serials(id);

CREATE INDEX ix_stock_movements_lot ON stock_movements (lot_id);
CREATE INDEX ix_stock_movements_serial ON stock_movements (serial_id);
