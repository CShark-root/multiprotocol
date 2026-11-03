# Creates the local test VM with Multipass (on top of Hyper-V): Ubuntu 22.04,
# 1 vCPU / 2 GB / 25 GB like the VPS, bridged to the home network, plus a
# "clean" snapshot. No installer to click through. See docs/vm-setup.md.
#
#   .\scripts\vm\create-vm-multipass.ps1
#   .\scripts\vm\create-vm-multipass.ps1 -Network "Wi-Fi"
param(
    [string]$Name = "panel-vm",
    [string]$Adapter = "Ethernet",   # physical adapter to bridge, see `multipass networks`
    [string]$Switch = "panel-ext"
)
$ErrorActionPreference = "Stop"
$mp = (Get-Command multipass -ErrorAction SilentlyContinue).Source
if (-not $mp) { $mp = "C:\Program Files\Multipass\bin\multipass.exe" }
$cloudInit = Join-Path $PSScriptRoot "cloud-init.yaml"

# Multipass only creates a bridge after an interactive prompt, so create the
# external switch ourselves (one UAC prompt; the network drops for a moment).
if (-not ((& $mp networks) -match "^$Switch\s")) {
    $cmd = "New-VMSwitch -Name '$Switch' -NetAdapterName '$Adapter' -AllowManagementOS `$true"
    Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList "-NoProfile", "-Command", $cmd
    Start-Sleep -Seconds 8
}

& $mp launch 22.04 --name $Name --cpus 1 --memory 2G --disk 25G `
    --network "name=$Switch,mode=auto" --cloud-init $cloudInit --timeout 1200
if ($LASTEXITCODE -ne 0) { throw "multipass launch failed" }

& $mp exec $Name -- cloud-init status --wait
& $mp stop $Name
& $mp snapshot $Name --name clean
& $mp start $Name
& $mp info $Name
Write-Host "Rollback any time: multipass stop $Name; multipass restore $Name.clean --destructive; multipass start $Name"
