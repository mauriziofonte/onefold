# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

<#
.SYNOPSIS
Measures how much of the first-catalog cost on Windows comes from Microsoft
Defender's on-access scan and how much from the cold OS file cache.

.DESCRIPTION
Runs the same read-only `onefold catalog` twice, each right after a reboot
(cold cache): once with Defender real-time protection as it is, once with a
process exclusion for onefold.exe. It reboots the machine twice by itself and
continues after each logon through a scheduled task that it removes at the
end, together with the exclusion. Nothing outside the results folder is
written; the measured tree is only read.

Run it from an administrator PowerShell:
  powershell -ExecutionPolicy Bypass -File scripts\defender-measure.ps1 -Onefold C:\path\to\onefold.exe

Results: %LOCALAPPDATA%\onefold-defender-measure\summary.txt (also printed at the end).
Save your work before starting: the machine reboots twice.

.PARAMETER Onefold
Full path of onefold.exe (the exclusion is made for this exact path).

.PARAMETER Tree
A large, non-personal folder Windows has not read recently. Default: the
Office installation used for the measurement already in the README.
#>
param(
    [Parameter(Mandatory = $true)][string]$Onefold,
    [string]$Tree = "C:\Program Files\Microsoft Office\root\Office16",
    [switch]$Resume
)

$ErrorActionPreference = "Stop"
$Work = Join-Path $env:LOCALAPPDATA "onefold-defender-measure"
$State = Join-Path $Work "state.txt"
$TaskName = "onefold-defender-measure"

function Assert-Admin {
    $id = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $id.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Run this script from an administrator PowerShell."
    }
}

function Register-Continue {
    $argLine = "-NoProfile -ExecutionPolicy Bypass -NoExit -File `"$PSCommandPath`" -Onefold `"$Onefold`" -Tree `"$Tree`" -Resume"
    $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $argLine
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
    $principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -RunLevel Highest -LogonType Interactive
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null
}

function Unregister-Continue {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
}

function Measure-Catalog([string]$label) {
    # give the system a minute after logon so startup I/O does not skew the run
    Write-Host "waiting 60 s for the logon I/O to settle..."
    Start-Sleep -Seconds 60
    $manifest = Join-Path $Work "$label.jsonl"
    $log = Join-Path $Work "$label.log"
    Remove-Item -Force -ErrorAction SilentlyContinue $manifest
    # onefold writes its progress and -perf table on stderr; Windows
    # PowerShell 5.1 turns each such line into an error record, which "Stop"
    # would make fatal: collect them as plain text instead.
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $wall = Measure-Command {
            & $Onefold catalog -preset all -allow-mixed -no-sweep -j 8 -perf -o $manifest $Tree 2>&1 |
                ForEach-Object { "$_" } | Set-Content -Path $log -Encoding UTF8
        }
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $prev
    }
    Add-Content -Path $log -Value ("exit code: {0}" -f $code) -Encoding UTF8
    Add-Content -Path $log -Value ("wall seconds: {0:N2}" -f $wall.TotalSeconds) -Encoding UTF8
    if ($code -ne 0) { throw "onefold catalog failed (exit code $code), see $log" }
    Write-Host ("{0}: {1:N2} s wall, log in {2}" -f $label, $wall.TotalSeconds, $log)
}

function Write-Summary {
    $out = Join-Path $Work "summary.txt"
    $lines = @(
        "onefold Defender measurement, $(Get-Date -Format s)",
        "onefold: $Onefold ($(& $Onefold version))",
        "tree: $Tree",
        "Windows: $([Environment]::OSVersion.VersionString)",
        "Defender real-time protection: $((Get-MpComputerStatus).RealTimeProtectionEnabled)",
        ""
    )
    foreach ($label in "cold-defender", "cold-excluded") {
        $log = Join-Path $Work "$label.log"
        $lines += "== $label"
        $lines += Get-Content $log | Where-Object { $_ -match "wall seconds|exit code|open|assets|files" }
        $lines += ""
    }
    Set-Content -Path $out -Value $lines -Encoding UTF8
    Write-Host ""
    Get-Content $out | Write-Host
    Write-Host ""
    Write-Host "Paste the content of $out into the project thread."
}

Assert-Admin
if (-not (Test-Path $Onefold)) { throw "onefold.exe not found: $Onefold" }
if (-not (Test-Path $Tree)) { throw "tree not found: $Tree" }
$Onefold = (Resolve-Path $Onefold).Path
New-Item -ItemType Directory -Force -Path $Work | Out-Null

$step = if ($Resume -and (Test-Path $State)) { Get-Content $State } else { "start" }
switch ($step) {
    "start" {
        Write-Host "Step 1/3: reboot for a cold cache. Save your work; rebooting in 30 s (Ctrl+C to cancel)."
        Set-Content $State "after-reboot-1"
        Register-Continue
        Start-Sleep -Seconds 30
        Restart-Computer -Force
    }
    "after-reboot-1" {
        Write-Host "Step 2/3: catalog with Defender as configured, cold cache."
        Measure-Catalog "cold-defender"
        Add-MpPreference -ExclusionProcess $Onefold
        Set-Content $State "after-reboot-2"
        Write-Host "Exclusion added for $Onefold. Rebooting in 30 s for the second cold run."
        Start-Sleep -Seconds 30
        Restart-Computer -Force
    }
    "after-reboot-2" {
        try {
            Write-Host "Step 3/3: catalog with the onefold process excluded, cold cache."
            Measure-Catalog "cold-excluded"
        } finally {
            Remove-MpPreference -ExclusionProcess $Onefold
            Unregister-Continue
            Remove-Item -Force $State
            Write-Host "Exclusion removed, scheduled task removed."
        }
        Write-Summary
    }
}
