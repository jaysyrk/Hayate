$ErrorActionPreference = "Stop"

# Configuration
$Repo = "username/hayate"
$BinaryName = "hayate.exe"

Write-Host ""
Write-Host "  _   _    _ __   __  _  _____  ___ " -ForegroundColor Cyan
Write-Host " | | | |  / \ \ / / / \|_   _|/ _ \" -ForegroundColor Cyan
Write-Host " | |_| | / _ \ \ V / / _ \ | | |  _/" -ForegroundColor Cyan
Write-Host " |  _  |/ ___ \ | | / ___ \| | | |  " -ForegroundColor Cyan
Write-Host " |_| |_/_/   \_\_|/_/   \_\_| \___|" -ForegroundColor Cyan
Write-Host " Swift Cross-Device File Transfer"
Write-Host ""

Write-Host "[*] Detecting Windows environment..." -ForegroundColor DarkGray

# Detect Architecture
$Arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -match "ARM") {
    $Arch = "arm64"
}
Write-Host "[*] Architecture detected: windows-$Arch" -ForegroundColor DarkGray

# Fetch latest release metadata
Write-Host "[*] Fetching latest release from GitHub..." -ForegroundColor DarkGray
$ReleaseUrl = "https://api.github.com/repos/$Repo/releases/latest"
$Release = Invoke-RestMethod -Uri $ReleaseUrl
$LatestTag = $Release.tag_name

if (-not $LatestTag) {
    Write-Host "[ERR] Failed to fetch latest release tag." -ForegroundColor Red
    exit 1
}

$AssetName = "hayate-windows-${Arch}.exe"
$DownloadUrl = "https://github.com/$Repo/releases/download/$LatestTag/$AssetName"

# Setup installation directory
$InstallDir = "$env:USERPROFILE\.hayate\bin"
if (-not (Test-Path -Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

$DestPath = "$InstallDir\$BinaryName"

# Download binary
Write-Host "[*] Downloading $AssetName ($LatestTag)..." -ForegroundColor DarkGray
Invoke-WebRequest -Uri $DownloadUrl -OutFile $DestPath

if (-not (Test-Path -Path $DestPath)) {
    Write-Host "[ERR] Download failed." -ForegroundColor Red
    exit 1
}

# Add to User PATH if not already present
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notmatch [regex]::Escape($InstallDir)) {
    Write-Host "[*] Adding $InstallDir to user PATH..." -ForegroundColor DarkGray
    $NewPath = "$UserPath;$InstallDir"
    [Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
    $env:PATH = "$env:PATH;$InstallDir"
}

Write-Host "[OK] Hayate $LatestTag installed successfully." -ForegroundColor Green
Write-Host "[*] You may need to restart your terminal for the PATH changes to take effect." -ForegroundColor DarkGray
Write-Host "[*] Run 'hayate --help' to get started." -ForegroundColor DarkGray
