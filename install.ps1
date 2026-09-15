<#
.SYNOPSIS
    Installs Hauler on Windows.

.DESCRIPTION
    Usage:
      - irm https://get.hauler.dev/install.ps1 | iex
      - .\install.ps1

    Install Specific Release:
      - $env:HAULER_VERSION = "1.0.0"; irm https://get.hauler.dev/install.ps1 | iex
      - $env:HAULER_VERSION = "1.0.0"; .\install.ps1

    Set Install Directory:
      - $env:HAULER_INSTALL_DIR = "C:\hauler"; .\install.ps1

    Set Hauler Directory:
      - $env:HAULER_DIR = "C:\Users\me\.hauler"; .\install.ps1

    Install from a Local Directory:
      - $env:HAULER_LOCAL_INSTALL = "C:\path\to\dir"; .\install.ps1

    Debug Usage:
      - $env:HAULER_DEBUG = "true"; .\install.ps1

    Uninstall Usage:
      - $env:HAULER_UNINSTALL = "true"; .\install.ps1

.LINK
    https://hauler.dev
.LINK
    https://github.com/hauler-dev/hauler
#>

$ErrorActionPreference = "Stop"

function Write-Verbose-Line($msg) { Write-Host $msg }
function Write-Info($msg) { Write-Host "`n[INFO] Hauler: $msg" }
function Write-Warn($msg) { Write-Host "`n[WARN] Hauler: $msg" -ForegroundColor Yellow }
function Write-Fatal($msg) { Write-Host "`n[ERROR] Hauler: $msg" -ForegroundColor Red; exit 1 }

if ($env:HAULER_DEBUG -eq "true") { Set-PSDebug -Trace 1 }

# start hauler preflight checks
Write-Info "Starting Preflight Checks..."

# tar.exe ships with Windows 10 1803+ / Windows 11; everything else used below is a built-in cmdlet
if (-not (Get-Command tar.exe -ErrorAction SilentlyContinue)) {
    Write-Fatal "tar.exe is required to install Hauler (ships with Windows 10 1803+ and Windows 11)"
}

# set install directory from argument or environment variable
$HAULER_INSTALL_DIR = if ($env:HAULER_INSTALL_DIR) { $env:HAULER_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\hauler" }

# ensure install directory exists and/or create it
if (-not (Test-Path $HAULER_INSTALL_DIR)) {
    try { New-Item -ItemType Directory -Path $HAULER_INSTALL_DIR -Force | Out-Null }
    catch { Write-Fatal "Failed to Create Install Directory: $HAULER_INSTALL_DIR" }
}

# set hauler directory from argument or environment variable
$HAULER_DIR = if ($env:HAULER_DIR) { $env:HAULER_DIR } else { Join-Path $env:USERPROFILE ".hauler" }

# uninstall hauler from argument or environment variable
if ($env:HAULER_UNINSTALL -eq "true") {
    # remove the hauler binary
    Remove-Item -Path (Join-Path $HAULER_INSTALL_DIR "hauler.exe") -Force -ErrorAction SilentlyContinue

    # remove the hauler directory
    Remove-Item -Path $HAULER_DIR -Recurse -Force -ErrorAction SilentlyContinue

    Write-Info "Successfully Uninstalled Hauler"
    exit 0
}

# set version environment variable
$HAULER_VERSION = $env:HAULER_VERSION
if (-not $HAULER_VERSION) {
    if ($env:HAULER_LOCAL_INSTALL) {
        # derive the version from the staged checksums file
        $checksumsMatch = Get-ChildItem -Path $env:HAULER_LOCAL_INSTALL -Filter "hauler_*_checksums.txt" -ErrorAction SilentlyContinue | Select-Object -First 1
        if (-not $checksumsMatch) {
            Write-Fatal "No hauler_<version>_checksums.txt found in HAULER_LOCAL_INSTALL: $env:HAULER_LOCAL_INSTALL"
        }
        $HAULER_VERSION = $checksumsMatch.Name -replace "^hauler_", "" -replace "_checksums\.txt$", ""
    } else {
        # attempt to retrieve the latest version from GitHub
        try {
            $latest = Invoke-RestMethod -Uri "https://api.github.com/repos/hauler-dev/hauler/releases/latest"
            $HAULER_VERSION = $latest.tag_name -replace "^v", ""
        } catch {
            Write-Fatal "HAULER_VERSION is unable to be detected and/or retrieved from GitHub. Please set: `$env:HAULER_VERSION"
        }
    }

    # exit if the version could not be detected
    if (-not $HAULER_VERSION) {
        Write-Fatal "HAULER_VERSION is unable to be detected and/or retrieved. Please set: `$env:HAULER_VERSION"
    }
}

# detect the architecture
switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64"   { $ARCH = "amd64" }
    "Arm64" { $ARCH = "arm64" }
    default { Write-Fatal "Unsupported Architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}

# start hauler installation
Write-Info "Starting Installation..."

# display the version, platform, and architecture
Write-Verbose-Line "- Version: v$HAULER_VERSION"
Write-Verbose-Line "- Platform: windows"
Write-Verbose-Line "- Architecture: $ARCH"
Write-Verbose-Line "- Install Directory: $HAULER_INSTALL_DIR"
Write-Verbose-Line "- Hauler Directory: $HAULER_DIR"

# ensure hauler directory exists and/or create it
if (-not (Test-Path $HAULER_DIR)) {
    try { New-Item -ItemType Directory -Path $HAULER_DIR -Force | Out-Null }
    catch { Write-Fatal "Failed to Create Hauler Directory: $HAULER_DIR" }
}

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) "hauler-install-$([guid]::NewGuid())"
New-Item -ItemType Directory -Path $workDir | Out-Null
Push-Location $workDir

try {
    $checksumsFile = "hauler_${HAULER_VERSION}_checksums.txt"
    $archiveFile = "hauler_${HAULER_VERSION}_windows_${ARCH}.tar.gz"

    # start hauler artifacts local copy or remote download
    if ($env:HAULER_LOCAL_INSTALL) {
        # start hauler artifacts copy
        Write-Info "Starting Local Copy..."
        Write-Verbose-Line "- Local Directory: $env:HAULER_LOCAL_INSTALL"

        # copy the checksum file
        try { Copy-Item -Path (Join-Path $env:HAULER_LOCAL_INSTALL $checksumsFile) -Destination . }
        catch { Write-Fatal "Failed to Copy: $checksumsFile" }

        # copy the archive file
        try { Copy-Item -Path (Join-Path $env:HAULER_LOCAL_INSTALL $archiveFile) -Destination . }
        catch { Write-Fatal "Failed to Copy: $archiveFile" }
    } else {
        # start hauler artifacts download
        Write-Info "Starting Download..."
        $baseUrl = "https://github.com/hauler-dev/hauler/releases/download/v$HAULER_VERSION"

        # download the checksum file
        try { Invoke-WebRequest -Uri "$baseUrl/$checksumsFile" -OutFile $checksumsFile }
        catch { Write-Fatal "Failed to Download: $checksumsFile" }

        # download the archive file
        try { Invoke-WebRequest -Uri "$baseUrl/$archiveFile" -OutFile $archiveFile }
        catch { Write-Fatal "Failed to Download: $archiveFile" }
    }

    # start hauler checksum verification
    Write-Info "Starting Checksum Verification..."

    # verify the Hauler checksum
    $checksumLine = Select-String -Path $checksumsFile -Pattern "\s$([regex]::Escape($archiveFile))$"
    if (-not $checksumLine) {
        Write-Fatal "Failed to Locate Checksum: $archiveFile"
    }
    $expectedChecksum = ($checksumLine.Line -split '\s+')[0].ToLower()
    $determinedChecksum = (Get-FileHash -Algorithm SHA256 -Path $archiveFile).Hash.ToLower()

    if ($determinedChecksum -eq $expectedChecksum) {
        Write-Verbose-Line "- Expected Checksum: $expectedChecksum"
        Write-Verbose-Line "- Determined Checksum: $determinedChecksum"
        Write-Verbose-Line "- Successfully Verified Checksum: $archiveFile"
    } else {
        Write-Verbose-Line "- Expected: $expectedChecksum"
        Write-Verbose-Line "- Determined: $determinedChecksum"
        Write-Fatal "Failed Checksum Verification: $archiveFile"
    }

    # uncompress the hauler archive
    tar -xzf $archiveFile
    if ($LASTEXITCODE -ne 0) { Write-Fatal "Failed to Extract: $archiveFile" }

    # install the hauler binary
    try { Copy-Item -Path "hauler.exe" -Destination $HAULER_INSTALL_DIR -Force }
    catch { Write-Fatal "Failed to Install Hauler: $HAULER_INSTALL_DIR" }
} finally {
    Pop-Location
    Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
}

# add hauler to the user's persistent PATH
$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if (";$userPath;" -notlike "*;$HAULER_INSTALL_DIR;*") {
    [Environment]::SetEnvironmentVariable("PATH", "$userPath;$HAULER_INSTALL_DIR", "User")
    $env:PATH = "$env:PATH;$HAULER_INSTALL_DIR"
    Write-Verbose-Line "- Added ${HAULER_INSTALL_DIR} to PATH (open a new terminal for other shells to see it)"
}

# display success message
Write-Info "Successfully Installed Hauler at $HAULER_INSTALL_DIR\hauler.exe"

# display availability message
Write-Info "Hauler v$HAULER_VERSION is now available for use!"

# display hauler docs message
Write-Verbose-Line "- Documentation: https://hauler.dev`n"
