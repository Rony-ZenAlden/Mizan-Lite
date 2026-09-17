#!/usr/bin/env bash
#
# Proves every Mizan Lite architecture rule can FAIL.
#
# A rule nobody has watched fail may match nothing — a mistyped glob passes silently forever. So this
# plants one violation per rule, requires archlint to report THAT rule, and removes the plant. It runs
# on every `make lite-ci`, so a later edit to arch-rules.yml that breaks a pattern is caught the same
# day rather than never.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

planted=()
# Ends in `return 0`: an EXIT trap's last status becomes the script's, and `[ -n "" ] && …` on an empty
# list is false — which made a fully successful run exit 1.
cleanup() {
  local f
  for f in "${planted[@]:-}"; do
    if [ -n "$f" ]; then rm -rf "$f"; fi
  done
  return 0
}
trap cleanup EXIT

# plant PATH CONTENT — write a Go file and remember to remove it (or its new directory).
plant() {
  local path="$1" content="$2" dir
  dir="$(dirname "$path")"
  if [ ! -d "$dir" ]; then mkdir -p "$dir"; planted+=("$dir"); else planted+=("$path"); fi
  printf '%s\n' "$content" > "$path"
}

drill() {
  local rule="$1" expect="$2"; shift 2
  plant "$@"
  local out
  if out="$(go run ./tools/archlint ./... 2>&1)"; then
    echo "SURVIVED: archlint passed with a planted '$rule' violation" >&2
    exit 1
  fi
  if ! grep -q -- "$expect" <<<"$out"; then
    echo "WRONG RULE: planted '$rule' but archlint reported something else:" >&2
    echo "$out" >&2
    exit 1
  fi
  echo "  caught  $rule"
  cleanup; planted=()
}

M=github.com/mizan-erp/mizan

drill lite-edition-boundary "Mizan Lite may import only" \
  internal/lite/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/platform/outbox\""

drill mizan-independent-of-lite "Mizan must not import Mizan Lite" \
  internal/platform/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/lite/paths\""

drill lite-app-entry "apps/lite may import only" \
  apps/lite/zz_drill.go "package main
import _ \"$M/internal/platform/config\""

drill lite-domain-purity "Lite domain packages may import only" \
  internal/lite/settings/domain/zz_drill.go "package domain
import _ \"$M/internal/platform/database\""

drill dialect-in-lite "only platform/database may be dialect-aware" \
  internal/lite/zzdrill/drill.go "package zzdrill
import _ \"$M/internal/platform/database/dialect\""

# A CALL. forbid-call matches call expressions, so `f := time.Now` (a function value, called later)
# would evade it — a limit of the rule kind, recorded rather than papered over; review is the defence.
drill time-now-in-lite "take a kernel/clock.Clock" \
  internal/lite/zzdrill/drill.go "package zzdrill
import \"time\"
var _ = time.Now()"

drill no-float-in-lite "float32/float64 are forbidden" \
  internal/lite/zzdrill/drill.go "package zzdrill
var _ float64"

drill no-sql-in-lite-service "SQL belongs to infra/platform" \
  internal/lite/settings/zz_drill.go "package settings
const drillQuery = \"SELECT value FROM settings WHERE setting_key = ?\""

# ── added in L1 ─────────────────────────────────────────────────────────────────────────────────────
drill mizan-cmd-independent-of-lite "Mizan must not import Mizan Lite" \
  cmd/demoseed/zz_drill.go "package main
import _ \"$M/internal/lite/paths\""

drill lite-pure-text "textkey, numinput, bizdate, tender and moneyfmt may import only" \
  internal/lite/textkey/zz_drill.go "package textkey
import _ \"$M/internal/platform/database\""

# moneyfmt joined lite-pure-text in L10. Planted in moneyfmt itself, not only in textkey: a rule that covers a package in
# its applies_to but is never seen failing THERE is a rule nobody has checked reaches it.
drill lite-pure-text-moneyfmt "textkey, numinput, bizdate, tender and moneyfmt may import only" \
  internal/lite/moneyfmt/zz_drill.go "package moneyfmt
import _ \"$M/internal/platform/database\""

drill lite-cmd-entry "Lite's commands may import only" \
  cmd/lite-demoseed/zz_drill.go "package main
import _ \"$M/internal/platform/config\""

drill lite-support-pure "support may import only" \
  internal/lite/support/zz_drill.go "package support
import _ \"$M/internal/lite/settings\""

drill lite-guide-pure "guide may import only" \
  internal/lite/guide/zz_drill.go "package guide
import _ \"$M/internal/lite/settings\""

drill lite-e2e-test-only "only cmd/lite-e2e may import internal/lite/e2e" \
  apps/lite/zz_drill.go "package main
import _ \"$M/internal/lite/e2e\""

drill lite-network-only-in-httpsource "only internal/lite/fx/infra/httpsource may open network connections" \
  internal/lite/printing/zz_drill.go "package printing
import _ \"net/http\""

drill lite-catalog-isolated "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/owner\""

drill lite-owner-isolated "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/catalog\""

drill lite-settings-isolated "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/owner\""

drill lite-setup-reaches-only-its-ports "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/catalog\""

# ── added in L2 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-stock-isolated "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/catalog\""

drill lite-stock-isolated-from-owner "stock may not import another Lite module" \
  internal/lite/stock/domain/zz_drill.go "package domain
import _ \"$M/internal/lite/owner/domain\""

drill lite-catalog-forbids-stock "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/stock\""

drill lite-owner-forbids-stock "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/stock\""

drill lite-settings-forbids-stock "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/stock\""

drill lite-setup-forbids-stock "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/stock\""

drill lite-bizdate-pure "textkey, numinput, bizdate, tender and moneyfmt may import only" \
  internal/lite/bizdate/zz_drill.go "package bizdate
import _ \"$M/internal/kernel/clock\""

# ── added in L3 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-fx-isolated "fx may not import another Lite module" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"$M/internal/lite/settings\""

drill lite-catalog-forbids-fx "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/fx\""

drill lite-owner-forbids-fx "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/fx\""

drill lite-settings-forbids-fx "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/fx/domain\""

drill lite-stock-forbids-fx "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/fx\""

drill lite-setup-forbids-fx "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/fx\""

drill lite-network-only-in-httpsource "only internal/lite/fx/infra/httpsource may open network connections" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"net/http\""

drill lite-network-in-demoseed "only internal/lite/fx/infra/httpsource may open network connections" \
  internal/lite/demoseed/zz_drill.go "package demoseed
import _ \"net\""

# ── added in L4 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-sales-isolated "sales may not import another Lite module" \
  internal/lite/sales/zz_drill.go "package sales
import _ \"$M/internal/lite/stock\""

drill lite-sales-domain-isolated "sales may not import another Lite module" \
  internal/lite/sales/domain/zz_drill.go "package domain
import _ \"$M/internal/lite/catalog/domain\""

drill lite-sales-isolated-from-fx "sales may not import another Lite module" \
  internal/lite/sales/infra/sqlite/zz_drill.go "package sqlite
import _ \"$M/internal/lite/fx/domain\""

drill lite-catalog-forbids-sales "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/sales\""

drill lite-owner-forbids-sales "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/sales\""

drill lite-settings-forbids-sales "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/sales\""

drill lite-stock-forbids-sales "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/sales\""

drill lite-fx-forbids-sales "fx may not import another Lite module" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"$M/internal/lite/sales\""

drill lite-setup-forbids-sales "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/sales\""

# ── added in L5 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-customers-isolated "customers may not import another Lite module" \
  internal/lite/customers/zz_drill.go "package customers
import _ \"$M/internal/lite/sales\""

drill lite-customers-domain-isolated "customers may not import another Lite module" \
  internal/lite/customers/domain/zz_drill.go "package domain
import _ \"$M/internal/lite/sales/domain\""

drill lite-customers-isolated-from-fx "customers may not import another Lite module" \
  internal/lite/customers/infra/sqlite/zz_drill.go "package sqlite
import _ \"$M/internal/lite/fx/domain\""

drill lite-tender-pure "textkey, numinput, bizdate, tender and moneyfmt may import only" \
  internal/lite/tender/zz_drill.go "package tender
import _ \"$M/internal/kernel/money\""

drill lite-sales-forbids-customers "sales may not import another Lite module" \
  internal/lite/sales/zz_drill.go "package sales
import _ \"$M/internal/lite/customers\""

drill lite-catalog-forbids-customers "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/customers\""

drill lite-owner-forbids-customers "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/customers\""

drill lite-settings-forbids-customers "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/customers\""

drill lite-stock-forbids-customers "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/customers\""

drill lite-fx-forbids-customers "fx may not import another Lite module" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"$M/internal/lite/customers\""

drill lite-setup-forbids-customers "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/customers\""

# ── added in L6 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-reports-isolated "reports may not import another Lite module" \
  internal/lite/reports/zz_drill.go "package reports
import _ \"$M/internal/lite/sales\""

drill lite-reports-domain-isolated "reports may not import another Lite module" \
  internal/lite/reports/domain/zz_drill.go "package domain
import _ \"$M/internal/lite/stock/domain\""

drill lite-reports-forbids-cashbook "reports may not import another Lite module" \
  internal/lite/reports/reportstest/zz_drill.go "package reportstest
import _ \"$M/internal/lite/cashbook\""

drill lite-cashbook-isolated "cashbook may not import another Lite module" \
  internal/lite/cashbook/zz_drill.go "package cashbook
import _ \"$M/internal/lite/reports\""

drill lite-cashbook-isolated-from-fx "cashbook may not import another Lite module" \
  internal/lite/cashbook/infra/sqlite/zz_drill.go "package sqlite
import _ \"$M/internal/lite/fx/domain\""

drill lite-catalog-forbids-reports "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/reports\""

drill lite-owner-forbids-cashbook "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/cashbook\""

drill lite-settings-forbids-reports "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/reports\""

drill lite-setup-forbids-cashbook "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/cashbook\""

drill lite-stock-forbids-reports "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/reports\""

drill lite-fx-forbids-cashbook "fx may not import another Lite module" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"$M/internal/lite/cashbook\""

drill lite-sales-forbids-reports "sales may not import another Lite module" \
  internal/lite/sales/zz_drill.go "package sales
import _ \"$M/internal/lite/reports\""

drill lite-customers-forbids-cashbook "customers may not import another Lite module" \
  internal/lite/customers/zz_drill.go "package customers
import _ \"$M/internal/lite/cashbook\""

drill lite-sales-forbids-cashbook "sales may not import another Lite module" \
  internal/lite/sales/zz_drill.go "package sales
import _ \"$M/internal/lite/cashbook\""

drill lite-stock-forbids-cashbook "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/cashbook/domain\""

# ── added in L7 ─────────────────────────────────────────────────────────────────────────────────────
drill lite-printing-isolated "printing may not import another Lite module" \
  internal/lite/printing/zz_drill.go "package printing
import _ \"$M/internal/lite/customers\""

drill lite-backups-isolated "backups may not import another Lite module" \
  internal/lite/backups/zz_drill.go "package backups
import _ \"$M/internal/lite/settings\""

drill lite-typeset-only "only internal/lite/typeset may import go-text" \
  internal/lite/documents/zz_drill.go "package documents
import _ \"github.com/go-text/typesetting/shaping\""

drill lite-typeset-only-image "only internal/lite/typeset may import go-text" \
  internal/lite/api/zz_drill.go "package api
import _ \"golang.org/x/image/vector\""

drill lite-os-calls-only-in-printers "only internal/lite/printers may use os/exec" \
  internal/lite/backups/zz_drill.go "package backups
import _ \"os/exec\""

drill lite-os-calls-syscall "only internal/lite/printers may use os/exec" \
  internal/lite/documents/zz_drill.go "package documents
import _ \"syscall\""

drill lite-documents-pure "documents and sheets may import only" \
  internal/lite/sheets/zz_drill.go "package sheets
import _ \"$M/internal/kernel/clock\""

drill lite-documents-pure-db "documents and sheets may import only" \
  internal/lite/documents/zz_drill.go "package documents
import _ \"$M/internal/platform/database\""

drill no-float-still-in-lite-api "float32/float64 are forbidden" \
  internal/lite/api/zz_drill.go "package api
var _ float32"

drill no-float-still-in-printers "float32/float64 are forbidden" \
  internal/lite/printers/zz_drill.go "package printers
var _ float64"

drill lite-catalog-forbids-printing "catalog may not import another Lite module" \
  internal/lite/catalog/zz_drill.go "package catalog
import _ \"$M/internal/lite/printing\""

drill lite-owner-forbids-backups "owner may not import another Lite module" \
  internal/lite/owner/zz_drill.go "package owner
import _ \"$M/internal/lite/backups\""

drill lite-settings-forbids-printing "settings may not import another Lite module" \
  internal/lite/settings/zz_drill.go "package settings
import _ \"$M/internal/lite/printing\""

drill lite-setup-forbids-backups "setup may reach other modules only through the ports it declares" \
  internal/lite/setup/zz_drill.go "package setup
import _ \"$M/internal/lite/backups\""

drill lite-stock-forbids-printing "stock may not import another Lite module" \
  internal/lite/stock/zz_drill.go "package stock
import _ \"$M/internal/lite/printing\""

drill lite-fx-forbids-backups "fx may not import another Lite module" \
  internal/lite/fx/zz_drill.go "package fx
import _ \"$M/internal/lite/backups\""

drill lite-sales-forbids-printing "sales may not import another Lite module" \
  internal/lite/sales/zz_drill.go "package sales
import _ \"$M/internal/lite/printing\""

drill lite-customers-forbids-printing "customers may not import another Lite module" \
  internal/lite/customers/zz_drill.go "package customers
import _ \"$M/internal/lite/printing\""

drill lite-cashbook-forbids-backups "cashbook may not import another Lite module" \
  internal/lite/cashbook/zz_drill.go "package cashbook
import _ \"$M/internal/lite/backups\""

drill lite-reports-forbids-printing "reports may not import another Lite module" \
  internal/lite/reports/zz_drill.go "package reports
import _ \"$M/internal/lite/printing\""

echo "every Lite architecture rule was seen failing"
