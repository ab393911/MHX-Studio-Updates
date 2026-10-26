# Per-task retry accounting for MHX Studio.
# Call once after each completed attempt. Does not infer win/loss from game images.
param(
 [Parameter(Mandatory=$true)][string]$TaskKey,
 [Parameter(Mandatory=$true)][ValidateSet('success','failure')] [string]$Result,
 [Nullable[int]]$MaxAttempts = $null,
 [string]$StateFile = "$PSScriptRoot\retry-state.json"
)
$ErrorActionPreference = 'Stop'
if ($null -ne $MaxAttempts -and $MaxAttempts -lt 1) { throw 'MaxAttempts must be at least 1.' }
if (-not $TaskKey.Trim()) { throw 'TaskKey cannot be blank.' }
$state = @{}
if (Test-Path $StateFile) {
 $loaded = Get-Content $StateFile -Raw | ConvertFrom-Json
 foreach ($p in $loaded.PSObject.Properties) { $state[$p.Name] = $p.Value }
}
# An explicit success clears history for this task; a failure increments it.
if ($Result -eq 'success') {
 $state.Remove($TaskKey)
 $action = 'completed'; $attempts = 0
} else {
 $attempts = 1
 if ($state.ContainsKey($TaskKey)) { $attempts = [int]$state[$TaskKey] + 1 }
 $state[$TaskKey] = $attempts
 if ($null -eq $MaxAttempts) { $action = 'needs_retry_limit' }
 elseif ($attempts -ge $MaxAttempts) { $action = 'skip' }
 else { $action = 'retry' }
}
$state | ConvertTo-Json -Depth 5 | Set-Content -Path $StateFile -Encoding UTF8
[pscustomobject]@{ TaskKey=$TaskKey; Result=$Result; Attempts=$attempts; MaxAttempts=$MaxAttempts; Action=$action } | ConvertTo-Json -Compress
# Caller should only advance after Action=completed or skip, and log skips.
