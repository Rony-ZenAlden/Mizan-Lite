# Optional GitHub Actions template

Mizan ERP is developed **fully locally and offline**. GitHub is NOT required.

The canonical CI is `scripts/check.sh` (or `make ci`), which runs everything on your
machine with no network once dependencies are cached.

`ci.yml.template` here is an **inert template**. It does nothing unless you deliberately
opt in by copying it to `.github/workflows/ci.yml` and pushing to a GitHub remote — a
future choice, never a requirement. Nothing in the project depends on it.
