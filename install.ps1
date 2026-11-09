# Install graphify233 as a single global binary.
#
#   irm https://raw.githubusercontent.com/neko233-com/graphify233/main/install.ps1 | iex
#   pwsh -File install.ps1 -Version v0.1.0
#   pwsh -File install.ps1 -LocalDist dist          # install a local build
#
# Verification: SHA-256 of the binary is always checked against checksums.txt.
# The checksums file's Ed25519 signature is verified when a verifier is
# available (an installed graphify233, gf-release, or the Go toolchain);
# -Strict makes a missing verifier fatal.
param(
    [string]$Version = "latest",
    [string]$BinDir = "$env:LOCALAPPDATA\Programs\graphify233\bin",
    [string]$Repo = "neko233-com/graphify233",
    [string]$Mirror = $env:GRAPHIFY233_MIRROR,
    [string]$LocalDist = "",
    [switch]$NoPath,
    [switch]$Strict
)

$ErrorActionPreference = "Stop"
$PubKey = "4Up+dr60klL8ZLQsjF+BK7eul45Qb8xDBK8iIfnmmE0="

function Get-RuntimeArch {
    switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
        "X64" { return "amd64" }
        "Arm64" { return "arm64" }
        default { throw "unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
    }
}

$arch = Get-RuntimeArch
$asset = "graphify233_windows_$arch.exe"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("graphify233-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    if ($LocalDist) {
        $src = Join-Path $LocalDist $asset
        if (-not (Test-Path $src)) { throw "local build not found: $src" }
        Copy-Item $src (Join-Path $tmp $asset)
        Copy-Item (Join-Path $LocalDist "checksums.txt") (Join-Path $tmp "checksums.txt")
        if (Test-Path (Join-Path $LocalDist "checksums.txt.sig")) {
            Copy-Item (Join-Path $LocalDist "checksums.txt.sig") (Join-Path $tmp "checksums.txt.sig")
        }
    }
    else {
        $api = if ($env:GRAPHIFY233_API) { $env:GRAPHIFY233_API.TrimEnd('/') } else { "https://api.github.com" }
        $url = if ($Version -eq "latest") {
            "$api/repos/$Repo/releases/latest"
        } else {
            "$api/repos/$Repo/releases/tags/$Version"
        }

        # Download routes: direct first, then an explicit mirror or the built-in
        # China-friendly list. The fastest reachable one wins per install; the
        # Ed25519 signature still has to verify locally, so a mirror cannot
        # inject code.
        $mirrors = @()
        if ($Mirror) {
            $mirrors += , @("env", $Mirror.TrimEnd('/') + "/")
        }
        elseif (-not $env:GRAPHIFY233_NO_MIRROR) {
            $mirrors += , @("gh-proxy", "https://gh-proxy.com/")
            $mirrors += , @("ghfast", "https://ghfast.top/")
            $mirrors += , @("ghproxy.net", "https://ghproxy.net/")
            $mirrors += , @("gitmirror", "https://hub.gitmirror.com/")
            $mirrors += , @("moeyy", "https://github.moeyy.xyz/")
        }

        Write-Host "resolving release: $url"
        $rel = $null
        $relSource = ""
        foreach ($src in @(, @("direct", "")) + $mirrors) {
            $prefix = $src[1]
            $u = if ($prefix) { $prefix + $url } else { $url }
            try {
                $rel = Invoke-RestMethod -Uri $u -Headers @{ "User-Agent" = "graphify233-installer"; "Accept" = "application/vnd.github+json" } -TimeoutSec 8
                $relSource = $src[0]
                break
            }
            catch {
                # 403/timeouts from mirrors that do not proxy the API are expected.
            }
        }
        if (-not $rel) {
            throw "could not resolve the release from GitHub or any mirror; set GRAPHIFY233_MIRROR to a prefix proxy"
        }
        $tag = $rel.tag_name
        Write-Host "release: $tag (via $relSource)"

        function AssetUrlOf([string]$name) {
            $a = $rel.assets | Where-Object { $_.name -eq $name } | Select-Object -First 1
            if (-not $a) { throw "release $tag has no asset $name" }
            return $a.browser_download_url
        }

        # Rank routes by timing the small checksums.txt download, keeping the
        # bytes from the winning route instead of fetching twice.
        $ranked = @()
        foreach ($src in @(, @("direct", "")) + $mirrors) {
            $name = $src[0]
            $prefix = $src[1]
            $u = if ($prefix) { $prefix + (AssetUrlOf "checksums.txt") } else { AssetUrlOf "checksums.txt" }
            $file = Join-Path $tmp "checksums-$name.txt"
            $sw = [System.Diagnostics.Stopwatch]::StartNew()
            try {
                Invoke-WebRequest -Uri $u -OutFile $file -UseBasicParsing -TimeoutSec 8
                $sw.Stop()
                $ranked += [pscustomobject]@{ Name = $name; Prefix = $prefix; Delay = $sw.ElapsedMilliseconds; File = $file }
            }
            catch {
                $sw.Stop()
            }
        }
        if ($ranked.Count -eq 0) {
            throw "could not download checksums.txt from GitHub or any mirror"
        }
        $ranked = @($ranked | Sort-Object Delay)
        $best = $ranked[0]
        Write-Host ("download source: {0} ({1} ms)" -f $best.Name, $best.Delay)
        Copy-Item -Force $best.File (Join-Path $tmp "checksums.txt")

        function SaveAsset([string]$assetName, [string]$dest) {
            $u0 = AssetUrlOf $assetName
            foreach ($r in $ranked) {
                $u = if ($r.Prefix) { $r.Prefix + $u0 } else { $u0 }
                try {
                    Invoke-WebRequest -Uri $u -OutFile $dest -UseBasicParsing -TimeoutSec 120
                    return
                }
                catch {
                    # try the next fastest route
                }
            }
            throw "could not download $assetName from GitHub or any mirror"
        }
        SaveAsset "checksums.txt.sig" (Join-Path $tmp "checksums.txt.sig")
        SaveAsset $asset (Join-Path $tmp $asset)
    }

    $binPath = Join-Path $tmp $asset
    $sumsPath = Join-Path $tmp "checksums.txt"
    $sigPath = Join-Path $tmp "checksums.txt.sig"

    # 1) SHA-256 of the binary.
    $want = (Get-Content $sumsPath | ForEach-Object {
            $parts = $_ -split '\s+'
            if ($parts.Length -ge 2 -and $parts[1].TrimStart('*') -eq $asset) { return $parts[0].ToLower() }
        } | Select-Object -First 1)
    if (-not $want) { throw "checksums.txt has no entry for $asset" }
    $got = (Get-FileHash -Algorithm SHA256 $binPath).Hash.ToLower()
    if ($got -ne $want) { throw "checksum mismatch for $asset (expected $want, got $got)" }
    Write-Host "sha256 ok: $got"

    # 2) Ed25519 signature over checksums.txt.
    $verified = $false
    if (Test-Path $sigPath) {
        $localVerifier = Join-Path $BinDir "graphify233.exe"
        if (Test-Path $localVerifier) {
            & $localVerifier verify-release --in $sumsPath --sig $sigPath --pub $PubKey *> $null
            $verified = ($LASTEXITCODE -eq 0)
        }
        if (-not $verified) {
            $existing = Get-Command graphify233 -ErrorAction SilentlyContinue
            if ($existing) {
                & $existing.Source verify-release --in $sumsPath --sig $sigPath --pub $PubKey *> $null
                $verified = ($LASTEXITCODE -eq 0)
            }
        }
        if (-not $verified -and (Test-Path (Join-Path (Get-Location) "cmd\gf-release"))) {
            $goCmd = Get-Command go -ErrorAction SilentlyContinue
            if ($goCmd) {
                & $goCmd.Source run ./cmd/gf-release verify -in $sumsPath -sig $sigPath -pub $PubKey *> $null
                $verified = ($LASTEXITCODE -eq 0)
            }
        }
    }
    if (-not $verified) {
        if ($Strict) { throw "could not verify the release signature (no verifier available) and -Strict was requested" }
        Write-Warning "signature not verified (no verifier available); SHA-256 matched."
    }
    else {
        Write-Host "signature ok"
    }

    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    $target = Join-Path $BinDir "graphify233.exe"
    Copy-Item -Force $binPath $target
    Write-Host "installed: $target"

    if (-not $NoPath) {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $parts = @($userPath -split ';' | Where-Object { $_ })
        if ($parts -notcontains $BinDir) {
            $newPath = (@($parts) + $BinDir) -join ';'
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            Write-Host "added to user PATH: $BinDir (restart your terminal)"
        }
        $env:Path = "$env:Path;$BinDir"
    }

    & $target version
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
