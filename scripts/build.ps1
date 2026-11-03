# Builds the web UI and the panel binaries on Windows:
#   bin\panel.exe        - for local development
#   bin\panel-linux-amd64 - for the server
param([string]$Version = "dev")
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Push-Location (Join-Path $root "web")
try {
    npm ci
    npm run build
} finally {
    Pop-Location
}

Push-Location $root
try {
    $ldflags = "-s -w -X panel/internal/shared/version.Version=$Version"
    go build -trimpath -ldflags $ldflags -o bin\panel.exe .\cmd\panel
    $env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags $ldflags -o bin\panel-linux-amd64 .\cmd\panel
} finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
Write-Host "Built bin\panel.exe and bin\panel-linux-amd64"
