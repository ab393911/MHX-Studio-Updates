# MHX Studio update helper (PowerShell 5.1+)
# Example: powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\Update-MHXStudio.ps1 -ManifestUrl "https://example.com/update.json" -ExePath "C:\MHXStudio\MHXStudio.exe"
param(
  [Parameter(Mandatory=$true)][string]$ManifestUrl,
  [Parameter(Mandatory=$true)][string]$ExePath
)
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
function Fail([string]$message) { throw $message }
if (-not $ManifestUrl.StartsWith("https://", [StringComparison]::OrdinalIgnoreCase)) { Fail "ManifestUrl must be HTTPS." }
$ExePath = [IO.Path]::GetFullPath($ExePath)
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) { Fail "Target EXE not found: $ExePath" }
if ([IO.Path]::GetExtension($ExePath).ToLowerInvariant() -ne ".exe") { Fail "Target must be an EXE." }

# Always verify the manifest and downloaded EXE before changing the installed program.
$manifest = Invoke-RestMethod -Uri $ManifestUrl -TimeoutSec 30
if (-not $manifest.version -or -not $manifest.download_url -or -not $manifest.sha256) { Fail "Manifest missing version, download_url or sha256." }
$url = [string]$manifest.download_url
$sha = ([string]$manifest.sha256).ToLowerInvariant()
if (-not $url.StartsWith("https://", [StringComparison]::OrdinalIgnoreCase)) { Fail "Package URL must be HTTPS." }
if ($sha -notmatch '^[0-9a-f]{64}$') { Fail "Invalid sha256." }

$folder = Split-Path -Parent $ExePath
$temp = Join-Path $folder (".mhx-download-" + [guid]::NewGuid().ToString("N") + ".exe")
$backup = "$ExePath.mhx-backup"
$installed = $false
try {
  Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $temp -TimeoutSec 300
  $actual = (Get-FileHash -LiteralPath $temp -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $sha) { Fail "SHA-256 mismatch. Update cancelled." }
  $fileInfo = Get-Item -LiteralPath $temp
  if ($fileInfo.Length -le 0) { Fail "Downloaded EXE is empty." }
  $proc = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $ExePath }
  if ($proc) { Fail "Close MHX Studio before installing the update." }
  if (Test-Path -LiteralPath $backup) {
    Fail "Previous backup exists. Remove it only after verifying the previous update."
  }
  Move-Item -LiteralPath $ExePath -Destination $backup -ErrorAction Stop
  try {
    Move-Item -LiteralPath $temp -Destination $ExePath -ErrorAction Stop
    $installed = $true
  } catch {
    if (-not (Test-Path -LiteralPath $ExePath)) { Move-Item -LiteralPath $backup -Destination $ExePath }
    throw
  }
  Write-Output "Installed version $($manifest.version). Backup retained at $backup. Run the new EXE to confirm it works."
} finally {
  if (Test-Path -LiteralPath $temp) { Remove-Item -LiteralPath $temp -Force }
  if (-not $installed -and -not (Test-Path -LiteralPath $ExePath) -and (Test-Path -LiteralPath $backup)) {
    Move-Item -LiteralPath $backup -Destination $ExePath
  }
}
