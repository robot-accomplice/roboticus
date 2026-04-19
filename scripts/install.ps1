# install.ps1 — Install or update the Roboticus autonomous agent runtime on Windows.
#
# Usage:
#   irm https://roboticus.ai/install.ps1 | iex
#   .\install.ps1 -Version v2026.04.10
#
# Installs to %LOCALAPPDATA%\roboticus\ and adds to user PATH.

[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$InstallDir = ""
)

$ErrorActionPreference = "Stop"

$Repo = "robot-accomplice/roboticus"
$BinaryName = "roboticus.exe"

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "roboticus"
}

function Get-Platform {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default {
            Write-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

function Get-LatestVersion {
    $url = "https://api.github.com/repos/$Repo/releases/latest"
    $response = Invoke-RestMethod -Uri $url -Headers @{ "Accept" = "application/vnd.github+json" }
    return $response.tag_name
}

function Get-ExpectedChecksum {
    param([string]$ChecksumFile, [string]$ArtifactName)
    $lines = Get-Content $ChecksumFile
    foreach ($line in $lines) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2 -and $parts[1] -eq $ArtifactName) {
            return $parts[0]
        }
    }
    Write-Error "No checksum found for $ArtifactName in SHA256SUMS.txt"
    exit 1
}

function Test-Checksum {
    param([string]$FilePath, [string]$ExpectedHash)
    $actual = (Get-FileHash -Path $FilePath -Algorithm SHA256).Hash.ToLower()
    if ($actual -ne $ExpectedHash.ToLower()) {
        Write-Error "Checksum verification failed!`n  expected: $ExpectedHash`n  got:      $actual"
        exit 1
    }
}

function Add-ToPath {
    param([string]$Dir)

    # Pre-v1.0.6 this function used substring matching
    # (`$userPath -notlike "*$Dir*"`) which is both false-positive and
    # false-negative prone. For example:
    #   Dir = "C:\roboticus"
    #   existing PATH contains "C:\roboticus-old"
    #   substring match says "already present" — we skip the add, so
    #   the install dir silently never gets on PATH.
    # Or:
    #   Dir = "C:\Tools"
    #   existing PATH contains "C:\ToolsPrev"
    #   again: false positive.
    #
    # v1.0.6: split PATH on `;` and compare entries exactly (case-
    # insensitive on Windows). Trailing backslashes are normalized so
    # "C:\roboticus" and "C:\roboticus\" are treated as the same entry.
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $userPath) { $userPath = "" }

    $normalizedTarget = $Dir.TrimEnd("\").ToLower()
    $entries = @()
    foreach ($raw in $userPath.Split(";")) {
        $trimmed = $raw.Trim()
        if ($trimmed) { $entries += $trimmed }
    }

    $alreadyPresent = $false
    foreach ($e in $entries) {
        if ($e.TrimEnd("\").ToLower() -eq $normalizedTarget) {
            $alreadyPresent = $true
            break
        }
    }

    if ($alreadyPresent) {
        Write-Host "$Dir already on user PATH; leaving PATH unchanged."
        return
    }

    # Rebuild PATH by appending the new entry, preserving existing
    # entries verbatim (we don't mutate casing or trailing-slash shape
    # of entries the operator may have added intentionally).
    $newUserPath = ($entries + $Dir) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
    $env:Path = "$env:Path;$Dir"
    Write-Host "Added $Dir to user PATH."
}

# Main install flow.
$Arch = Get-Platform
$Artifact = "roboticus-windows-${Arch}.exe"

Write-Host "Detected platform: windows/$Arch"

# Determine version.
if (-not $Version) {
    Write-Host "Fetching latest version..."
    $Version = Get-LatestVersion
}

if (-not $Version) {
    Write-Error "Failed to determine version."
    exit 1
}
Write-Host "Installing roboticus $Version..."

$BaseUrl = "https://github.com/$Repo/releases/download/$Version"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) "roboticus-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    # Download binary.
    Write-Host "Downloading $Artifact..."
    $binaryPath = Join-Path $TempDir $Artifact
    Invoke-WebRequest -Uri "$BaseUrl/$Artifact" -OutFile $binaryPath -UseBasicParsing

    # Download checksums.
    Write-Host "Downloading SHA256SUMS.txt..."
    $checksumPath = Join-Path $TempDir "SHA256SUMS.txt"
    Invoke-WebRequest -Uri "$BaseUrl/SHA256SUMS.txt" -OutFile $checksumPath -UseBasicParsing

    # Verify checksum.
    $expectedHash = Get-ExpectedChecksum -ChecksumFile $checksumPath -ArtifactName $Artifact
    Write-Host "Verifying checksum..."
    Test-Checksum -FilePath $binaryPath -ExpectedHash $expectedHash
    Write-Host "Checksum verified."

    # Install.
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $destPath = Join-Path $InstallDir $BinaryName
    Copy-Item -Path $binaryPath -Destination $destPath -Force

    # Add to PATH.
    Add-ToPath -Dir $InstallDir

    Write-Host ""
    Write-Host "roboticus installed to $destPath"
    & $destPath version
}
finally {
    Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
}
