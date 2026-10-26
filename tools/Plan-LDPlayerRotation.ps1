# MHX Studio batch rotation planner
# This module plans sequential five-instance batches; game-task completion must be reported by the game controller.
param(
 [string]$InstallDir = 'D:\LDPlayer14',
 [int]$BatchSize = 5,
 [int]$StartIndex = 0,
 [string]$StateFile = "$PSScriptRoot\rotation-state.json",
 [switch]$AdvanceAfterVerifiedCompletion
)
$ErrorActionPreference = 'Stop'
$exe = @('dnconsole.exe','ldconsole.exe') | ForEach-Object { Join-Path $InstallDir $_ } | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $exe) { throw "LDPlayer console executable not found in $InstallDir" }
$rows = & $exe list2 2>&1
if ($LASTEXITCODE -ne 0) { throw "list2 failed: $($rows -join ' ')" }
$instances = @()
foreach ($row in $rows) {
 $p = ([string]$row).Trim() -split ',',7
 if ($p.Count -lt 7) { continue }
 $i = 0
 if ([int]::TryParse($p[0],[ref]$i)) { $instances += [pscustomobject]@{Index=$i;Name=$p[1]} }
}
$all = @($instances | Sort-Object Index -Unique | Where-Object { $_.Index -ge $StartIndex })
if ($all.Count -eq 0) { throw 'No available LDPlayer instances.' }
$batches = @()
for ($n=0; $n -lt $all.Count; $n += $BatchSize) {
 $end=[Math]::Min($n+$BatchSize-1,$all.Count-1)
 $group=@($all[$n..$end])
 if ($group.Count -ne $BatchSize) { Write-Warning "Skipping incomplete final group: $(($group.Index) -join ',')"; break }
 $batches += ,@($group.Index)
}
if (-not $batches.Count) { throw 'No full batch available.' }
$state = @{ Batch = 0; Status = 'pending'; Batches = $batches; UpdatedAt = (Get-Date).ToString('o') }
if (Test-Path $StateFile) {
 $old = Get-Content $StateFile -Raw | ConvertFrom-Json
 $newSignature = ($batches | ForEach-Object { $_ -join ',' }) -join ';'
 $oldSignature = (@($old.Batches) | ForEach-Object { @($_) -join ',' }) -join ';'
 if ($newSignature -eq $oldSignature) { $state.Batch=[int]$old.Batch; $state.Status=[string]$old.Status }
}
if ($AdvanceAfterVerifiedCompletion) {
 if ($state.Status -ne 'verified_complete') { throw 'Cannot advance: current batch is not verified_complete. No switching performed.' }
 $state.Batch++
 $state.Status='pending'
}
if ($state.Batch -ge $batches.Count) { Write-Host 'All batches completed.'; exit 0 }
$state.UpdatedAt = (Get-Date).ToString('o')
$state | ConvertTo-Json -Depth 8 | Set-Content $StateFile -Encoding UTF8
$selected=@($batches[$state.Batch])
Write-Host ("Batch {0}/{1}: LDPlayer indices {2}; status {3}" -f ($state.Batch+1),$batches.Count,($selected -join ','),$state.Status)
Write-Host 'No launch/close action is performed by this planner. Game controller must verify all servers and characters before updating status to verified_complete.'
