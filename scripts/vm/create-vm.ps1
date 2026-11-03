#Requires -RunAsAdministrator
# Creates the local test VM in Hyper-V with the same resources as the VPS
# (1 vCPU, 2 GB RAM, 25 GB disk), bridged to the home network so a phone on
# the same Wi-Fi can connect to it. See docs/vm-setup.md.
#
#   .\scripts\vm\create-vm.ps1 -IsoPath C:\Users\me\Downloads\ubuntu-22.04.5-live-server-amd64.iso
param(
    [Parameter(Mandatory)] [string]$IsoPath,
    [string]$Name = "panel-vm",
    [string]$SwitchName = "panel-ext",
    [string]$NetAdapter = "Ethernet",
    [string]$VmDir = "$env:PUBLIC\Documents\Hyper-V\panel-vm"
)
$ErrorActionPreference = "Stop"

if (-not (Get-Command New-VM -ErrorAction SilentlyContinue)) {
    throw "Hyper-V is not enabled yet: see step 1 in docs/vm-setup.md"
}
if (-not (Test-Path $IsoPath)) { throw "ISO not found: $IsoPath" }
if (Get-VM -Name $Name -ErrorAction SilentlyContinue) { throw "VM '$Name' already exists" }

if (-not (Get-VMSwitch -Name $SwitchName -ErrorAction SilentlyContinue)) {
    Write-Host "Creating external switch '$SwitchName' on '$NetAdapter' (the network drops for a few seconds)..."
    New-VMSwitch -Name $SwitchName -NetAdapterName $NetAdapter -AllowManagementOS $true | Out-Null
}

New-Item -ItemType Directory -Force $VmDir | Out-Null
$vhd = Join-Path $VmDir "$Name.vhdx"
New-VM -Name $Name -Generation 2 -MemoryStartupBytes 2GB -NewVHDPath $vhd -NewVHDSizeBytes 25GB `
    -SwitchName $SwitchName -Path $VmDir | Out-Null
Set-VM -Name $Name -ProcessorCount 1 -StaticMemory -AutomaticCheckpointsEnabled $false
# Ubuntu boots with Secure Boot only under the third-party UEFI CA template.
Set-VMFirmware -VMName $Name -SecureBootTemplate MicrosoftUEFICertificateAuthority
$dvd = Add-VMDvdDrive -VMName $Name -Path $IsoPath -Passthru
Set-VMFirmware -VMName $Name -FirstBootDevice $dvd

Start-VM -Name $Name
vmconnect.exe localhost $Name
Write-Host "VM '$Name' started. Finish the Ubuntu installer in the window that opened."
