# build.ps1 — build ICURconDaemon and/or ICU RCON Client release binaries.
#
# Usage:
#   .\build.ps1                 # build both for the current platform
#   .\build.ps1 -Client -Linux  # build the client for linux-amd64 as well
#   .\build.ps1 -Version v1.2.0
param(
    [switch]$Daemon,
    [switch]$Client,
    [switch]$Linux,
    [string]$Version = "v1.1.0"
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

if (-not $Daemon -and -not $Client) {
    $Daemon = $true
    $Client = $true
}

# Ensure-Syso embeds build\icon.ico (plus product/version info) into a Go
# package as rsrc_windows_*.syso, so the next `go build` produces Windows
# binaries with the icon. Requires: go install github.com/tc-hib/go-winres@latest
function Ensure-Syso([string]$pkgDir, [string]$productName, [string]$fileDesc, [string]$origName) {
    $ico = Join-Path $root "build\icon.ico"
    if (-not (Test-Path $ico)) {
        Write-Host "   (build\icon.ico not found — building without embedded icon)" -ForegroundColor Yellow
        return
    }
    $winres = Join-Path $env:GOPATH "bin\go-winres.exe"
    if (-not $winres -or -not (Test-Path $winres)) {
        $winres = (Get-Command "go-winres.exe" -ErrorAction SilentlyContinue).Source
    }
    if (-not $winres -or -not (Test-Path $winres)) {
        Write-Host "   (go-winres not found — run: go install github.com/tc-hib/go-winres@latest)" -ForegroundColor Yellow
        return
    }
    $ver = $Version.TrimStart("v") + ".0"
    Push-Location $pkgDir
    try {
        & $winres simply --icon $ico --product-name $productName --file-description $fileDesc `
            --product-version $ver --file-version $ver --original-filename $origName | Out-Null
        Write-Host "   icon embedded (rsrc_windows syso refreshed)"
    } finally {
        Pop-Location
    }
}

function Build-Daemon([string]$os, [string]$ext) {
    Write-Host "== daemon $os-amd64" -ForegroundColor Cyan
    if ($os -eq "windows") { Ensure-Syso $root "ICU RCON Daemon" "ICU RCON Daemon" "ICURconDaemon-windows-amd64.exe" }
    $out = Join-Path $root "ICURconDaemon-$os-amd64$ext"
    # CGO_ENABLED=0 keeps the daemon fully static on Linux (no glibc dependency).
    $env:CGO_ENABLED = "0"
    go -C $root build -trimpath -ldflags "-s -w -X main.Version=$Version" -o $out .
    if ($LASTEXITCODE -ne 0) { throw "daemon build failed" }
    Write-Host "   -> $out"
}

function Build-Client([string]$os, [string]$ext, [string[]]$extraLd) {
    Write-Host "== client $os-amd64" -ForegroundColor Cyan
    if ($os -eq "windows") { Ensure-Syso (Join-Path $root "client") "ICU RCON Client" "ICU RCON Client" "ICURconClient-windows-amd64.exe" }
    $out = Join-Path $root "ICURconClient-$os-amd64$ext"
    $ld = @("-s", "-w", "-X", "main.clientVersion=$Version") + $extraLd
    go -C (Join-Path $root "client") build -trimpath -ldflags ($ld -join " ") -o $out .
    if ($LASTEXITCODE -ne 0) { throw "client build failed" }
    Write-Host "   -> $out"
}

if ($Daemon) {
    Build-Daemon "windows" ".exe"
    Build-Daemon "linux" ""
}
if ($Client) {
    # Windows client builds with CGO disabled (pure Go + bundled font).
    Build-Client "windows" ".exe" @("-H=windowsgui")
    # Linux/macOS clients need CGO (Gio's GL backend). CI builds the Linux
    # client on ubuntu-22.04 (glibc >= 2.35). Locally:
    #   linux:   CGO_ENABLED=1 go build -tags novulkan -trimpath -ldflags "-s -w -X main.clientVersion=$Version" -o ICURconClient .
    #   macos:   same command (requires Xcode Command Line Tools)
    Write-Host "Linux/macOS client must be built on the target OS (Gio GL backend needs CGO)." -ForegroundColor Yellow
    if ($Linux) {
        Write-Host "Hint: docker run --rm -v `"${root}:/src`" -w /src/client ubuntu:20.04 bash -c `"apt-get update && apt-get install -y libgl1-mesa-dev xorg-dev wget ca-certificates && wget -qO /tmp/go.tgz https://go.dev/dl/go1.24.0.linux-amd64.tar.gz && tar -C /usr/local -xzf /tmp/go.tgz && /usr/local/go/bin/go build -trimpath -ldflags '-s -w -X main.clientVersion=$Version' -o ../ICURconClient-linux-amd64 .`" (glibc >= 2.31)" -ForegroundColor Yellow
    }
}
Write-Host "done." -ForegroundColor Green
