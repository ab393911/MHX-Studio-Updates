$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force dist | Out-Null
$report = [ordered]@{ status='preparing'; basic=$null; ocr=$null; error=$null; started_at=[DateTime]::UtcNow.ToString('o') }
function Save-ProvisionReport { $report | ConvertTo-Json | Set-Content -Encoding utf8 dist/ocr-provision.json }
Save-ProvisionReport
try {
  # Language features depend on their Basic language capability.
  # This modifies only the disposable CI VM, never the user's PC.
  foreach ($kind in @('Basic', 'OCR')) {
    $name = "Language.$kind~~~zh-CN~0.0.1.0"
    $cap = Get-WindowsCapability -Online -Name $name -ErrorAction Stop
    $report.status = "checking-$kind"
    $report[$kind.ToLowerInvariant()] = [string]$cap.State
    Save-ProvisionReport
    if ($cap.State -ne 'Installed') {
      $report.status = "installing-$kind"
      Save-ProvisionReport
      Write-Host "Installing $name from Windows Update on the test runner"
      Add-WindowsCapability -Online -Name $name -ErrorAction Stop | Out-Null
    }
    $cap = Get-WindowsCapability -Online -Name $name -ErrorAction Stop
    $report[$kind.ToLowerInvariant()] = [string]$cap.State
    Save-ProvisionReport
    if ($cap.State -ne 'Installed') { throw "$name was not installed" }
  }
  $report.status = 'installed'
} catch {
  $report.status = 'failed'
  $report.error = $_.Exception.Message
  throw
} finally {
  Save-ProvisionReport
  if (Test-Path "$env:windir/Logs/DISM/dism.log") {
    Get-Content "$env:windir/Logs/DISM/dism.log" -Tail 200 | Set-Content -Encoding utf8 dist/ocr-provision-dism.log
  }
}
