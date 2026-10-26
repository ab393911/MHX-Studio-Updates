# Select five ONLINE ADB devices for one work cycle; other instances are untouched.
# Run with: powershell -ExecutionPolicy Bypass -File .\Select-LDPlayerBatch.ps1
param([string]$AdbPath = "D:\LDPlayer14\adb.exe", [string]$OutputPath = "$PSScriptRoot\active-batch.json")
$ErrorActionPreference = 'Stop'
if (!(Test-Path $AdbPath)) {
  $candidates = @("D:\LDPlayer14\adb.exe", "D:\LDPlayer14\adb\adb.exe", "D:\LDPlayer14\vms\adb.exe")
  $AdbPath = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
  if (!$AdbPath) {
    $found = Get-Command adb -ErrorAction SilentlyContinue
    if ($found) { $AdbPath = $found.Source } else { throw "ADB not found. Supply -AdbPath with the actual adb.exe location." }
  }
}
& $AdbPath start-server | Out-Null
$rows = @(& $AdbPath devices -l)
$serials = @($rows | Where-Object { $_ -match '^([^\s]+)\s+device(?:\s|$)' } | ForEach-Object { $Matches[1] } | Select-Object -Unique)
if ($serials.Count -lt 5) { throw "Only $($serials.Count) online ADB devices were found. Check ADB local connection on the running simulators." }
Write-Host "All online ADB devices ($($serials.Count)):"
for ($i=0; $i -lt $serials.Count; $i++) { Write-Host ("[{0}] {1}" -f ($i + 1), $serials[$i]) }
Write-Host "Enter EXACTLY five display numbers separated by commas, e.g. 1,2,3,4,5"
$raw = Read-Host "This cycle"
$parts = @($raw -split ',' | ForEach-Object { $_.Trim() })
if ($parts.Count -ne 5) { throw 'Select exactly five numbers.' }
$numbers = @()
foreach ($part in $parts) {
  $n = 0
  if (![int]::TryParse($part, [ref]$n) -or $n -lt 1 -or $n -gt $serials.Count) { throw "Invalid device number: $part" }
  $numbers += $n
}
if (@($numbers | Select-Object -Unique).Count -ne 5) { throw "Duplicate selections are not allowed." }
$selected = @($numbers | ForEach-Object { $serials[$_ - 1] })
$entry = [ordered]@{ timestamp = (Get-Date).ToString('o'); adbPath = $AdbPath; devices = $selected }
$entry | ConvertTo-Json -Depth 4 | Set-Content -Encoding UTF8 -Path $OutputPath
Write-Host "Saved five selected ADB serials to $OutputPath"
Write-Host "Only the selected serials should be targeted with adb -s SERIAL commands."
