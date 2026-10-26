# Read LDPlayer instance state; never assume the machine only has five instances.
param(
  [string]$InstallDir = 'D:\LDPlayer14',
  [int[]]$Indices = @(),
  [int]$ExpectedCount = 5
)
$ErrorActionPreference = 'Stop'
$exe = @('dnconsole.exe','ldconsole.exe') | ForEach-Object { Join-Path $InstallDir $_ } | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $exe) { throw "Cannot find dnconsole.exe or ldconsole.exe under $InstallDir" }
$lines = & $exe list2 2>&1
if ($LASTEXITCODE -ne 0) { throw "LDPlayer list2 failed: $($lines -join ' ')" }
$instances = @()
foreach ($line in $lines) {
  $parts = ([string]$line).Trim() -split ',', 7
  if ($parts.Count -lt 7) { continue }
  $index = 0; $android = 0; $pid = 0
  if (-not [int]::TryParse($parts[0], [ref]$index)) { continue }
  [void][int]::TryParse($parts[4], [ref]$android)
  [void][int]::TryParse($parts[5], [ref]$pid)
  $instances += [pscustomobject]@{
    Index=$index; Name=$parts[1]; AndroidStarted=$android; ProcessId=$pid;
    Running=($pid -gt 0); Ready=($pid -gt 0 -and $android -eq 1)
  }
}
if (-not $instances.Count) { throw 'No LDPlayer instances returned by list2.' }
$running = @($instances | Where-Object Running | Sort-Object Index)
Write-Host ("Found {0} total instances; {1} currently running." -f $instances.Count,$running.Count)
$running | Format-Table Index,Name,AndroidStarted,ProcessId,Ready -AutoSize | Out-Host
if ($Indices.Count -gt 0) {
  $chosen = @($running | Where-Object { $Indices -contains $_.Index } | Sort-Object Index)
  if ($chosen.Count -ne $Indices.Count) { throw 'Some selected indices are not running. No operation performed.' }
} elseif ($running.Count -eq $ExpectedCount) {
  $chosen = $running
} else {
  throw "Expected $ExpectedCount running instances, found $($running.Count). Pass -Indices 1,2,3,4,5 to select the intended round. No operation performed."
}
if ($chosen.Count -ne $ExpectedCount) { throw "Selected $($chosen.Count), need $ExpectedCount. No operation performed." }
if (@($chosen | Where-Object { -not $_.Ready }).Count) { throw 'A selected instance is not Android-ready. No operation performed.' }
Write-Host ('Selected round: ' + (($chosen | ForEach-Object Index) -join ', '))
# Output objects with the exact LDPlayer instance indices for the launch / ADB binding stage.
$chosen
