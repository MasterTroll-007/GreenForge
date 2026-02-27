# ============================================================
# GreenForge - Windows Installer (PowerShell)
# ============================================================
# Usage:
#   .\scripts\install.ps1
#   OR
#   iwr -useb https://greenforge.dev/install.ps1 | iex
# ============================================================

$ErrorActionPreference = "Stop"
$GreenforgeDIr = if ($env:GREENFORGE_DIR) { $env:GREENFORGE_DIR } else { "$env:USERPROFILE\.greenforge" }

function Write-Info($msg)  { Write-Host "[INFO] $msg" -ForegroundColor Cyan }
function Write-Ok($msg)    { Write-Host "[OK] $msg" -ForegroundColor Green }
function Write-Warn($msg)  { Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err($msg)   { Write-Host "[ERROR] $msg" -ForegroundColor Red; exit 1 }

Write-Host ""
Write-Host "╔══════════════════════════════════════════╗" -ForegroundColor Green
Write-Host "║     🔧 GreenForge Installer              ║" -ForegroundColor Green
Write-Host "║     Secure AI Agent for JVM Teams         ║" -ForegroundColor Green
Write-Host "╚══════════════════════════════════════════╝" -ForegroundColor Green
Write-Host ""

# 1. Check prerequisites
Write-Info "Checking prerequisites..."

# Docker
try {
    $dockerVersion = docker --version
    Write-Ok "Docker found: $dockerVersion"
} catch {
    Write-Err "Docker is required. Install from https://docs.docker.com/desktop/install/windows-install/"
}

# Docker Compose
try {
    $composeVersion = docker compose version --short
    Write-Ok "Docker Compose found: $composeVersion"
} catch {
    Write-Err "Docker Compose not found. Update Docker Desktop."
}

# Docker daemon
try {
    docker info | Out-Null
    Write-Ok "Docker daemon is running"
} catch {
    Write-Err "Docker daemon is not running. Start Docker Desktop first."
}

# 2. Create directories
Write-Info "Setting up: $GreenforgeDIr"
$dirs = @("ca", "certs", "index", "tools", "sessions", "workspace")
foreach ($d in $dirs) {
    New-Item -ItemType Directory -Path "$GreenforgeDIr\$d" -Force | Out-Null
}

# 3. Copy source
$srcDir = "$GreenforgeDIr\src"
if (Test-Path "$srcDir\docker-compose.yml") {
    Write-Info "GreenForge source exists. Updating..."
} else {
    Write-Info "Copying GreenForge source..."
    $scriptDir = Split-Path -Parent $PSScriptRoot
    if (Test-Path "$scriptDir\docker-compose.yml") {
        Copy-Item -Path $scriptDir -Destination $srcDir -Recurse -Force
    } else {
        Write-Err "Cannot find GreenForge source. Clone the repo first."
    }
}

Set-Location $srcDir

# 4. Build and start
Write-Info "Building GreenForge Docker image..."
docker compose build gf

Write-Info "Starting GreenForge..."
docker compose up -d gf

# 5. Wait for health
Write-Info "Waiting for GreenForge..."
$ready = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $health = Invoke-RestMethod -Uri "http://localhost:18789/api/v1/health" -ErrorAction SilentlyContinue
        if ($health.status -eq "ok") { $ready = $true; break }
    } catch {}
    Start-Sleep -Seconds 1
}

if ($ready) {
    Write-Ok "GreenForge is running!"
} else {
    Write-Warn "GreenForge may still be starting. Check: docker compose logs -f gf"
}

# 6. Create CLI wrapper
$wrapperPath = "$env:USERPROFILE\.local\bin\greenforge.cmd"
New-Item -ItemType Directory -Path (Split-Path $wrapperPath) -Force | Out-Null
@"
@echo off
docker exec -it greenforge greenforge %*
"@ | Out-File -FilePath $wrapperPath -Encoding ascii

Write-Ok "CLI wrapper: $wrapperPath"

# Check PATH
if ($env:PATH -notlike "*$env:USERPROFILE\.local\bin*") {
    Write-Warn "Add $env:USERPROFILE\.local\bin to your PATH"
}

# 7. Done
Write-Host ""
Write-Host "╔══════════════════════════════════════════╗" -ForegroundColor Green
Write-Host "║     ✅ GreenForge Installed!              ║" -ForegroundColor Green
Write-Host "╚══════════════════════════════════════════╝" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. greenforge init        # Setup wizard"
Write-Host "  2. greenforge run         # Interactive session"
Write-Host "  3. http://localhost:18789 # Web UI"
Write-Host ""
