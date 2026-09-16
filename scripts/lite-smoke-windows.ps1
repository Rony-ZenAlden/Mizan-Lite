# Opens an installed Mizan Lite on a copy of a shop, waits for it to reach Go, and closes it (L8 D-L8.5, §7 step 6).
#
#   powershell -ExecutionPolicy Bypass -File scripts\lite-smoke-windows.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\lite-smoke-windows.ps1 -Exe "C:\Program Files\Mizan\Mizan Lite\Mizan Lite.exe" -Fixture "D:\a shop"
param(
  [string]$Exe = "$env:ProgramFiles\Mizan\Mizan Lite\Mizan Lite.exe",
  [string]$Fixture = ""
)
$ErrorActionPreference = "Stop"

if (-not (Test-Path $Exe)) { throw "Mizan Lite is not installed at $Exe" }

$data = Join-Path ([System.IO.Path]::GetTempPath()) ("mizan-lite-smoke-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $data | Out-Null
if ($Fixture -ne "") {
  Copy-Item -Path (Join-Path $Fixture "*") -Destination $data -Recurse
  Write-Host "> a copy of $Fixture"
  $wanted = "health requested by the frontend"
} else {
  Write-Host "> a new shop"
  $wanted = "mizan lite ready"
}
$log = Join-Path $data "logs\mizan-lite.log"

$env:MIZAN_LITE_DATA_DIR = $data
$app = Start-Process -FilePath $Exe -PassThru
try {
  $found = $false
  for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Seconds 1
    if ((Test-Path $log) -and (Select-String -Path $log -Pattern $wanted -Quiet)) { $found = $true; break }
  }
  if (-not $found) {
    if (Test-Path $log) { Get-Content $log -Tail 20 }
    throw "the application never logged '$wanted' — the window did not reach Go"
  }
} finally {
  if (-not $app.HasExited) { $app.CloseMainWindow() | Out-Null; Start-Sleep -Seconds 5 }
  if (-not $app.HasExited) { $app | Stop-Process -Force }
}

$errors  = (Select-String -Path $log -Pattern '"level":"ERROR"' -AllMatches | Measure-Object).Count
$backups = (Get-ChildItem (Join-Path $data "backups") -Filter *.db -ErrorAction SilentlyContinue | Measure-Object).Count
$version = (Select-String -Path $log -Pattern 'mizan lite ready.*"version":"([^"]+)"' | Select-Object -Last 1).Matches.Groups[1].Value

Write-Host "  data:    $data"
Write-Host "  version: $version"
Write-Host "  backups: $backups"
Write-Host "  errors:  $errors"
if ($errors -ne 0) { throw "the log holds $errors errors" }
Write-Host "OK the installed application opened, reached Go, and closed cleanly"
