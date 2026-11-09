# Build, checksum, sign and (optionally) publish graphify233 releases.
#
#   pwsh -File release.ps1 -Version v0.1.0
#   pwsh -File release.ps1 -Version v0.1.0 -Publish
#
# Outputs go to dist/: one static binary per platform, checksums.txt and
# checksums.txt.sig (Ed25519). Releases contain exactly one binary per
# platform; project data stays in each project's graphify-out/ directory.
param(
    [string]$Version = "v0.1.0",
    [string]$OutDir = "dist",
    [switch]$Publish,
    [switch]$NoSign
)

$ErrorActionPreference = "Stop"
$repo = "neko233-com/graphify233"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
    function Find-Go {
        if ($env:GO) { return $env:GO }
        $cmd = Get-Command go -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
        foreach ($p in @("$env:USERPROFILE\toolchains\go\bin\go.exe", "C:\Program Files\Go\bin\go.exe")) {
            if (Test-Path $p) { return $p }
        }
        throw "Go toolchain not found. Install Go 1.27+ or set GO."
    }
    $go = Find-Go

    $keyDir = Join-Path $root "release\keys"
    $keyPath = Join-Path $keyDir "ed25519.key"
    $pubPath = Join-Path $keyDir "ed25519.pub"
    if (-not (Test-Path $keyPath)) {
        Write-Host "no signing key found; generating one at $keyPath"
        & $go run ./cmd/gf-release keygen -key $keyPath
        if ($LASTEXITCODE -ne 0) { throw "keygen failed" }
        Write-Host "Commit release/keys/ed25519.pub and keep ed25519.key secret."
    }
    $pub = (Get-Content $pubPath -Raw).Trim()
    Write-Host "signing key: $pub"

    if (Test-Path $OutDir) { Remove-Item -Recurse -Force $OutDir }
    New-Item -ItemType Directory -Path $OutDir | Out-Null

    $ldflags = "-s -w -X github.com/$repo/internal/selfupdate.Version=$Version -X github.com/$repo/internal/selfupdate.PublicKeyB64=$pub"
    $targets = @(
        @{ os = "windows"; arch = "amd64"; ext = ".exe" },
        @{ os = "windows"; arch = "arm64"; ext = ".exe" },
        @{ os = "linux";   arch = "amd64"; ext = "" },
        @{ os = "linux";   arch = "arm64"; ext = "" },
        @{ os = "darwin";  arch = "amd64"; ext = "" },
        @{ os = "darwin";  arch = "arm64"; ext = "" }
    )
    $saved = @{
        GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED
    }
    try {
        foreach ($t in $targets) {
            $name = "graphify233_$($t.os)_$($t.arch)$($t.ext)"
            Write-Host "building $name"
            $env:GOOS = $t.os
            $env:GOARCH = $t.arch
            $env:CGO_ENABLED = "0"
            & $go build -trimpath -buildvcs=false -ldflags $ldflags -o (Join-Path $OutDir $name) ./cmd/graphify233
            if ($LASTEXITCODE -ne 0) { throw "build failed for $name" }
        }
    }
    finally {
        foreach ($k in $saved.Keys) {
            if ($null -eq $saved[$k]) { Remove-Item "env:$k" -ErrorAction SilentlyContinue }
            else { Set-Item "env:$k" $saved[$k] }
        }
    }

    & $go run ./cmd/gf-release checksums -dir $OutDir
    if ($LASTEXITCODE -ne 0) { throw "checksums failed" }

    if (-not $NoSign) {
        & $go run ./cmd/gf-release sign -key $keyPath -in (Join-Path $OutDir "checksums.txt") -out (Join-Path $OutDir "checksums.txt.sig")
        if ($LASTEXITCODE -ne 0) { throw "signing failed" }
    }

    Write-Host ""
    Write-Host "release artifacts in ${OutDir}:"
    Get-ChildItem $OutDir | ForEach-Object { Write-Host ("  {0,-34} {1,12:N0} bytes" -f $_.Name, $_.Length) }

    if ($Publish) {
        $gh = Get-Command gh -ErrorAction SilentlyContinue
        if (-not $gh) { throw "gh CLI is required for -Publish" }
        $notes = "graphify233 $Version - single static binary, zero dependencies."
        # PowerShell does not expand wildcards for native commands, so pass the
        # asset paths explicitly (gh release create takes one path per argument).
        $assets = Get-ChildItem -Path $OutDir -File | ForEach-Object { $_.FullName }
        & gh release create $Version @assets --repo $repo --title "graphify233 $Version" --notes $notes
        if ($LASTEXITCODE -ne 0) { throw "gh release create failed" }
        Write-Host "published $Version"
    }
}
finally {
    Pop-Location
}
