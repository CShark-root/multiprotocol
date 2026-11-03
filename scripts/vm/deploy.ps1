# Builds the panel and installs (or updates) it on the test VM over SSH.
#
#   .\scripts\vm\deploy.ps1 -Target dmitr@192.168.1.50
#   .\scripts\vm\deploy.ps1 -Target dmitr@192.168.1.50 -SkipBuild   # reuse bin\panel-linux-amd64
param(
    [Parameter(Mandatory)] [string]$Target,
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
    "deploy\panel.service",
    "deploy\remote-install.sh"
) | ForEach-Object { Join-Path $root $_ }

scp @files "${Target}:/tmp/"
if ($LASTEXITCODE -ne 0) { throw "scp failed" }
# -t: sudo asks for the password interactively.
ssh -t $Target "sudo bash /tmp/remote-install.sh /tmp"
