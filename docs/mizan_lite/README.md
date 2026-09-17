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
| **L1** | Catalogue, units, owner PIN, demo data generator | [phases/L1_CATALOGUE.md](phases/L1_CATALOGUE.md) | ✅ complete — `edb4570` |
| **L2** | Stock: receipts, adjustments, weighted-average cost, opening packages | [phases/L2_STOCK.md](phases/L2_STOCK.md) | ✅ complete — `407fdbb` |
| **L3** | Exchange rates and the currency system | [phases/L3_RATES.md](phases/L3_RATES.md) | ✅ complete — `67d72c8` |
| **L4** | The till: sales, checkout, receipts | [phases/L4_TILL.md](phases/L4_TILL.md) | ✅ complete — `76463fd` |
| **L5** | Customers and debts | [phases/L5_CUSTOMERS.md](phases/L5_CUSTOMERS.md) | ✅ complete — `97b7875` |
| **L6** | Reports: profit, stock value and the cash drawer | [phases/L6_REPORTS.md](phases/L6_REPORTS.md) | ✅ complete — `2c86f17` |
| **L7** | Export (Excel, PDF), thermal printing, backup and restore | [phases/L7_HARDWARE_BACKUP.md](phases/L7_HARDWARE_BACKUP.md) | ✅ committed — `d356598`; printer and Excel checks with the owner |
| **L8** | End-to-end testing, polish, release and deployment | [phases/L8_RELEASE.md](phases/L8_RELEASE.md) · [phases/L8_DOD_REVIEW.md](phases/L8_DOD_REVIEW.md) | ✅ committed, tagged `lite-v0.9.0` — the Windows protocol, paper and Excel with the owner |

| **L9** | Cost price and profit margin; the stocktake | [phases/L9_COST_AND_AUDIT.md](phases/L9_COST_AND_AUDIT.md) · [phases/L9_AUDIT_DRAFT.sql](phases/L9_AUDIT_DRAFT.sql) | 🔨 cost price shipped in 0.9.2; the stocktake designed, not built |
| **L10** | Dropping the two noughts: one money pipeline, three readings of the pound | [DECISIONS.md](DECISIONS.md) — D-L10.1–5 | ✅ shipped, tagged `lite-v0.9.5` |

## Conventions

- **Dates are absolute** (`2026-09-13`), never "yesterday".
- **Nothing is reported as passed that did not run.** A check that could not run says NOT RUN and why.
- **Findings about Mizan** discovered while building Lite are recorded in the phase record that found them and
  listed in [PROGRESS.md](PROGRESS.md) §4, so Mizan's owner can act on them without reading Lite's code.

---

## The shop's documents

Written for the shop, not for this repository: Arabic first, English beside it (L8 D-L8.16). The application ships them as PDFs
(About → Guides), rendered from these files by `make lite-guides`.

| Document | Reader |
|---|---|
| [guide/SHOP_GUIDE.ar.md](guide/SHOP_GUIDE.ar.md) · [guide/SHOP_GUIDE.md](guide/SHOP_GUIDE.md) | the shopkeeper |
| [guide/QUICK_CARD.ar.md](guide/QUICK_CARD.ar.md) | the counter, printed and kept beside the till |
| [guide/INSTALL.ar.md](guide/INSTALL.ar.md) · [guide/INSTALL.md](guide/INSTALL.md) | whoever installs it |
| [guide/TROUBLESHOOTING.ar.md](guide/TROUBLESHOOTING.ar.md) · [guide/TROUBLESHOOTING.md](guide/TROUBLESHOOTING.md) | both |
| [GLOSSARY.md](GLOSSARY.md) | one term for one thing, in both languages |
| [RELEASE.md](RELEASE.md) | whoever cuts a release |
| [phases/WINDOWS_PROTOCOL.md](phases/WINDOWS_PROTOCOL.md) · [phases/PILOT.md](phases/PILOT.md) | the owner, on a real machine and in a real shop |
| [phases/L8_DOD_REVIEW.md](phases/L8_DOD_REVIEW.md) | whoever asks what is still owed before 1.0.0 |
