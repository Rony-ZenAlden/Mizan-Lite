# Mizan Lite — documentation

Everything written about Mizan Lite lives in this folder, and only here: the design, every decision, every
phase's design note and implementation record, and the running progress log. `docs/architecture/` holds
Mizan's documents; nothing about Lite belongs there.

`internal/lite/docsgate` enforces this: it fails if a Lite document appears outside this folder, or if a file
in this folder is not linked from this index.

---

## Start here

| Document | What it is |
|---|---|
| [PROGRESS.md](PROGRESS.md) | **The resume point.** Where the project stands, what is open, what is next. Updated at the end of every step. |
| [DESIGN.md](DESIGN.md) | The approved edition design. Read **§13 (amendments)** first — it records the owner's answers and every change made since approval. |
| [DECISIONS.md](DECISIONS.md) | The decision register: every decision, who approved it, where it is argued, and whether it still stands. |

## Phases

Each phase has one document. It opens as a **design note** (status *proposed*), stops for approval, and is
extended into the **implementation record** — what was built, the evidence, every mutation drill, findings, and
what is not verified — once built.

| Phase | Scope | Document | Status |
|---|---|---|---|
| **L0** | Skeleton and gates | [phases/L0_SKELETON.md](phases/L0_SKELETON.md) | ✅ complete — `c2a1d0e` |
| **L1** | Catalogue, units, owner PIN, demo data generator | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) | ✅ complete — committed |
| L2 | Stock: receipts, adjustments, weighted-average cost, opening packages | — | not started |
| L3 | Exchange rates (manual) | — | not started |
| L4 | The till | — | not started |
| L5 | Customers and debts | — | not started |
| L6 | Profit | — | not started |
| L7 | Receipts (printing) | — | not started |
| L8 | Release | — | not started |

## Conventions

- **Dates are absolute** (`2026-09-13`), never "yesterday".
- **Nothing is reported as passed that did not run.** A check that could not run says NOT RUN and why.
- **Findings about Mizan** discovered while building Lite are recorded in the phase record that found them and
  listed in [PROGRESS.md](PROGRESS.md) §4, so Mizan's owner can act on them without reading Lite's code.
