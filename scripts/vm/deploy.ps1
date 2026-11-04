# Builds the panel and installs (or updates) it on the test VM.
#
#   .\scripts\vm\deploy.ps1                           # Multipass VM "panel-vm"
#   .\scripts\vm\deploy.ps1 -SkipBuild                # reuse bin\panel-linux-amd64
#   .\scripts\vm\deploy.ps1 -Target user@192.168.1.50 # any server over SSH
param(
    [string]$Vm = "panel-vm",
    [string]$Target,
    [switch]$SkipBuild
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

if (-not $SkipBuild) {
    & (Join-Path $root "scripts\build.ps1")
}
$files = @(
    "bin\panel-linux-amd64",
    "scripts\install-xray.sh",
    "scripts\install-hysteria.sh",
    "scripts\install-singbox.sh",
    "deploy\panel.service",
    "deploy\panel-amnezia.service",
    "deploy\panel-update.service",
    "deploy\panel-update.path",
    "deploy\panel-update.sh",
    "install.sh",
    "deploy\remote-install.sh"
) | ForEach-Object { Join-Path $root $_ }

if ($Target) {
    scp @files "${Target}:/tmp/"
    if ($LASTEXITCODE -ne 0) { throw "scp failed" }
    # -t: sudo asks for the password interactively.
    ssh -t $Target "sudo bash /tmp/remote-install.sh /tmp"
    return
}

$mp = (Get-Command multipass -ErrorAction SilentlyContinue).Source
if (-not $mp) { $mp = "C:\Program Files\Multipass\bin\multipass.exe" }

# The VM has two addresses: Multipass's internal 172.16.0.0/12 one and the
# bridged LAN one. Share links must use the LAN address so phones can connect.
$info = (& $mp info $Vm --format json | ConvertFrom-Json).info.$Vm
$lan = $info.ipv4 | Where-Object { $_ -notmatch '^172\.(1[6-9]|2\d|3[01])\.' } | Select-Object -First 1
if (-not $lan) { throw "no LAN address on $Vm - is it bridged? (multipass info $Vm)" }
Write-Host "VM LAN address: $lan"

& $mp exec $Vm -- mkdir -p /tmp/panel-deploy
& $mp transfer @files "${Vm}:/tmp/panel-deploy/"
if ($LASTEXITCODE -ne 0) { throw "multipass transfer failed" }
& $mp exec $Vm -- sudo env "PUBLIC_HOST=$lan" bash /tmp/panel-deploy/remote-install.sh /tmp/panel-deploy
