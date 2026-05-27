# ModernPath CLI installer for Windows
# Usage:
#   irm https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.ps1 | iex
#   .\install.ps1 -Version 0.1.2
#   .\install.ps1 -InstallDir "C:\Tools\modernpath"

param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\modernpath",
    [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"

$Repo = "ModernPath/cli"
$AssetName = "modernpath-windows-amd64.zip"
$BinaryName = "modernpath.exe"

function Write-Step($Message) {
    Write-Host "→ $Message" -ForegroundColor Cyan
}

function Write-Ok($Message) {
    Write-Host "✓ $Message" -ForegroundColor Green
}

function Write-Warn($Message) {
    Write-Host "⚠ $Message" -ForegroundColor Yellow
}

function Get-LatestVersion {
    $headers = @{ "User-Agent" = "modernpath-installer" }
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers
    return $release.tag_name.TrimStart("v")
}

function Add-ToUserPath([string]$Directory) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -split ";" | Where-Object { $_ -eq $Directory }) {
        return $false
    }
    $newPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $Directory } else { "$userPath;$Directory" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = "$env:Path;$Directory"
    return $true
}

Write-Host ""
Write-Host "ModernPath CLI — Windows installer" -ForegroundColor White
Write-Host ""

if ($Version -eq "latest") {
    Write-Step "Resolving latest release..."
    $Version = Get-LatestVersion
}

$tag = "v$Version"
$downloadUrl = "https://github.com/$Repo/releases/download/$tag/$AssetName"
$checksumUrl = "https://github.com/$Repo/releases/download/$tag/checksums.txt"

Write-Step "Installing modernpath $Version to $InstallDir"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$tempZip = Join-Path $env:TEMP "modernpath-$Version.zip"
$tempExtract = Join-Path $env:TEMP "modernpath-extract-$Version"

try {
    Write-Step "Downloading $downloadUrl"
    Invoke-WebRequest -Uri $downloadUrl -OutFile $tempZip -UseBasicParsing

    try {
        Write-Step "Verifying checksum..."
        $checksums = (Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing).Content
        $expected = ($checksums -split "`n" | Where-Object { $_ -match [regex]::Escape($AssetName) }) -replace '.*\s+', '' -replace '\s.*', ''
        if ($expected) {
            $actual = (Get-FileHash -Path $tempZip -Algorithm SHA256).Hash.ToLower()
            if ($actual -ne $expected.ToLower()) {
                throw "Checksum mismatch for $AssetName"
            }
            Write-Ok "Checksum verified"
        }
    } catch {
        Write-Warn "Skipping checksum verification ($($_.Exception.Message))"
    }

    if (Test-Path $tempExtract) {
        Remove-Item -Recurse -Force $tempExtract
    }
    New-Item -ItemType Directory -Force -Path $tempExtract | Out-Null

    Write-Step "Extracting..."
    Expand-Archive -Path $tempZip -DestinationPath $tempExtract -Force

    $sourceExe = Join-Path $tempExtract $BinaryName
    if (-not (Test-Path $sourceExe)) {
        throw "Expected $BinaryName inside archive"
    }

    $destExe = Join-Path $InstallDir $BinaryName
    Copy-Item -Path $sourceExe -Destination $destExe -Force

    if (-not $NoPathUpdate) {
        if (Add-ToUserPath $InstallDir) {
            Write-Ok "Added $InstallDir to user PATH (open a new terminal to use modernpath)"
        } else {
            Write-Ok "$InstallDir is already on user PATH"
        }
    }

    Write-Host ""
    Write-Ok "Installed modernpath $Version"
    Write-Host "  Binary: $destExe" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor White
    Write-Host "  modernpath --version"
    Write-Host "  modernpath auth"
    Write-Host "  cd your-project && modernpath init"
    Write-Host ""
} finally {
    if (Test-Path $tempZip) { Remove-Item -Force $tempZip -ErrorAction SilentlyContinue }
    if (Test-Path $tempExtract) { Remove-Item -Recurse -Force $tempExtract -ErrorAction SilentlyContinue }
}
