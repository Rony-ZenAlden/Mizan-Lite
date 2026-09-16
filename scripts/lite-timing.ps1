# Measures on the shop's own Windows machine what cannot be measured on the build machine (L8 §7 step 13, PROGRESS O6 and O8):
# how long the owner PIN takes to check, and how long a month's report and a year of sales history take.
#
#   powershell -ExecutionPolicy Bypass -File scripts\lite-timing.ps1
#
# It needs the repository and Go on the machine; without them, report the figures the application's own log shows instead.
$ErrorActionPreference = "Stop"
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
  Write-Host "> the owner PIN (Argon2id), a month's report, a year of sales history"
  go test -run "TestAYearOfSalesReportsInTime|TestAYearOfSalesHistoryExportsInTime|TestThePINIsCheckedInReasonableTime" -v ./internal/lite/bootstrap/ ./internal/lite/api/ ./internal/lite/owner/... 2>&1 |
    Select-String -Pattern "^\s+\w+_test.go:.*: .*(ms|s)$|--- (PASS|FAIL)"
  Write-Host ""
  Write-Host "Write these figures into docs/mizan_lite/phases/WINDOWS_PROTOCOL.md, step 13."
} finally {
  Pop-Location
}
