# seek installer script for Windows (PowerShell)
# Usage:
#   irm https://raw.githubusercontent.com/ozgurulukir/seek/main/install.ps1 | iex

$ErrorActionPreference = 'Stop'

# Enable TLS 1.2
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo = "ozgurulukir/seek"

function Write-Info($message) {
    Write-Host "==> " -ForegroundColor Blue -NoNewline
    Write-Host $message
}

function Write-Success($message) {
    Write-Host "✓ " -ForegroundColor Green -NoNewline
    Write-Host $message
}

function Write-Warn($message) {
    Write-Host "! " -ForegroundColor Yellow -NoNewline
    Write-Host $message
}

# 1. Architecture Check
if (-not [System.Environment]::Is64BitOperatingSystem) {
    Write-Error "seek is only available for 64-bit Windows."
    exit 1
}

# 2. Version Resolution
$Version = $env:SEEK_VERSION
if (-not $Version) {
    Write-Info "Checking for latest release..."
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "seek-installer" }
        $Version = $release.tag_name
    } catch {
        # Fallback to redirect resolution if API is rate-limited
        try {
            $req = [System.Net.HttpWebRequest]::Create("https://github.com/$Repo/releases/latest")
            $req.AllowAutoRedirect = $false
            $resp = $req.GetResponse()
            $location = $resp.GetResponseHeader("Location")
            $resp.Close()
            $Version = ($location -split "/")[-1]
        } catch {
            Write-Error "Failed to determine latest version: $_"
            exit 1
        }
    }
}

if (-not $Version) {
    Write-Error "Could not resolve seek version. Set `$env:SEEK_VERSION = 'vX.Y.Z' and re-run."
    exit 1
}

# 3. Target Installation Directory
$InstallDir = if ($env:SEEK_INSTALL) { $env:SEEK_INSTALL } else { Join-Path $env:LOCALAPPDATA "seek\bin" }
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$ArchiveName = "seek_${Version}_windows-amd64.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Version/$ArchiveName"
$ChecksumUrl = "https://github.com/$Repo/releases/download/$Version/SHA256SUMS.txt"

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("seek-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

try {
    $ZipPath = Join-Path $TempDir $ArchiveName
    $ChecksumPath = Join-Path $TempDir "SHA256SUMS.txt"

    Write-Info "Installing seek $Version (windows/amd64)..."
    Write-Info "Downloading $DownloadUrl..."

    # Use BITS or Invoke-WebRequest
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath -UseBasicParsing

    # Try downloading checksums
    try {
        Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing -ErrorAction SilentlyContinue
        if (Test-Path $ChecksumPath) {
            Write-Info "Verifying checksum..."
            $actualHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLower()
            $expectedLine = Get-Content $ChecksumPath | Where-Object { $_ -match $ArchiveName }
            if ($expectedLine) {
                $expectedHash = ($expectedLine -split '\s+')[0].ToLower()
                if ($actualHash -ne $expectedHash) {
                    throw "Checksum mismatch! Expected: $expectedHash, Actual: $actualHash"
                }
                Write-Success "Checksum verified."
            }
        }
    } catch {
        Write-Warn "Checksum check skipped: $_"
    }

    Write-Info "Extracting..."
    Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

    $ExeSource = (Get-ChildItem -Path $TempDir -Filter "seek.exe" -Recurse | Select-Object -First 1).FullName
    if (-not $ExeSource) {
        throw "Could not find seek.exe in extracted archive."
    }

    $TargetExe = Join-Path $InstallDir "seek.exe"
    Move-Item -Path $ExeSource -Destination $TargetExe -Force
    Write-Success "Installed seek to $TargetExe"

    # 4. PATH Configuration
    $UserPath = [System.Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::User)
    if ($UserPath -notlike "*$InstallDir*") {
        $NewUserPath = "$InstallDir;$UserPath"
        [System.Environment]::SetEnvironmentVariable("Path", $NewUserPath, [System.EnvironmentVariableTarget]::User)
        $env:Path = "$InstallDir;$env:Path"
        Write-Success "Added $InstallDir to your user PATH."
    }

    # 5. Output Verification
    $InstalledVer = & $TargetExe --version 2>$null
    Write-Host ""
    Write-Host "seek $InstalledVer installed successfully!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Run 'seek --help' to get started!" -ForegroundColor White
} finally {
    Remove-Item -Recurse -Force -Path $TempDir -ErrorAction SilentlyContinue
}
