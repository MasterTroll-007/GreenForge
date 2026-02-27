#!/usr/bin/env bash
# ============================================================
# GreenForge - One-Click Installer
# ============================================================
# Usage:
#   curl -sSL https://greenforge.dev/install.sh | sh
#   OR
#   ./scripts/install.sh
# ============================================================

set -euo pipefail

GREENFORGE_DIR="${GREENFORGE_DIR:-$HOME/.greenforge}"
REPO_URL="https://github.com/greencode/greenforge"
VERSION="${GREENFORGE_VERSION:-latest}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║     🔧 GreenForge Installer              ║${NC}"
echo -e "${GREEN}║     Secure AI Agent for JVM Teams         ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
echo ""

# 1. Check prerequisites
info "Checking prerequisites..."

# Docker
if ! command -v docker &>/dev/null; then
    error "Docker is required but not installed. Install from https://docs.docker.com/get-docker/"
fi
ok "Docker found: $(docker --version | head -1)"

# Docker Compose
if docker compose version &>/dev/null; then
    ok "Docker Compose found: $(docker compose version --short)"
elif command -v docker-compose &>/dev/null; then
    ok "Docker Compose (legacy) found"
else
    error "Docker Compose is required. Update Docker Desktop or install docker-compose."
fi

# Check Docker daemon
if ! docker info &>/dev/null; then
    error "Docker daemon is not running. Start Docker Desktop first."
fi
ok "Docker daemon is running"

# Git (optional, for cloning)
if command -v git &>/dev/null; then
    ok "Git found: $(git --version)"
else
    warn "Git not found. Will download release archive instead."
fi

# 2. Create GreenForge directory
info "Setting up GreenForge directory: $GREENFORGE_DIR"
mkdir -p "$GREENFORGE_DIR"/{ca,certs,index,tools,sessions,workspace}

# 3. Clone or download
if [ -d "$GREENFORGE_DIR/src" ] && [ -f "$GREENFORGE_DIR/src/docker-compose.yml" ]; then
    info "GreenForge source already exists. Updating..."
    cd "$GREENFORGE_DIR/src"
    git pull --quiet 2>/dev/null || true
else
    info "Downloading GreenForge..."
    if command -v git &>/dev/null; then
        git clone --quiet --depth 1 "$REPO_URL" "$GREENFORGE_DIR/src" 2>/dev/null || {
            # If repo doesn't exist yet (pre-release), use local copy
            if [ -f "./docker-compose.yml" ]; then
                info "Using local source..."
                cp -r . "$GREENFORGE_DIR/src"
            else
                error "Could not download GreenForge. Check your internet connection."
            fi
        }
    fi
fi

cd "$GREENFORGE_DIR/src"

# 4. Build and start
info "Building GreenForge Docker image..."
docker compose build --quiet gf 2>/dev/null || docker compose build gf

info "Starting GreenForge..."
docker compose up -d gf

# 5. Wait for health
info "Waiting for GreenForge to be ready..."
for i in $(seq 1 30); do
    if curl -sf http://localhost:18789/api/v1/health &>/dev/null; then
        break
    fi
    sleep 1
done

if curl -sf http://localhost:18789/api/v1/health &>/dev/null; then
    ok "GreenForge is running!"
else
    warn "GreenForge may still be starting. Check: docker compose logs -f gf"
fi

# 6. Create CLI wrapper
info "Creating CLI wrapper..."
WRAPPER_PATH="/usr/local/bin/greenforge"
if [ -w "$(dirname "$WRAPPER_PATH")" ] 2>/dev/null; then
    cat > "$WRAPPER_PATH" << 'WRAPPER'
#!/usr/bin/env bash
docker exec -it greenforge greenforge "$@"
WRAPPER
    chmod +x "$WRAPPER_PATH"
    ok "CLI installed: greenforge"
else
    WRAPPER_PATH="$HOME/.local/bin/greenforge"
    mkdir -p "$(dirname "$WRAPPER_PATH")"
    cat > "$WRAPPER_PATH" << 'WRAPPER'
#!/usr/bin/env bash
docker exec -it greenforge greenforge "$@"
WRAPPER
    chmod +x "$WRAPPER_PATH"
    ok "CLI installed: $WRAPPER_PATH"
    warn "Make sure $HOME/.local/bin is in your PATH"
fi

# 7. Done!
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║     ✅ GreenForge Installed!              ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════╝${NC}"
echo ""
echo "Next steps:"
echo "  1. Run the setup wizard:"
echo "     greenforge init"
echo ""
echo "  2. Start an interactive session:"
echo "     greenforge run"
echo ""
echo "  3. Open the web UI:"
echo "     http://localhost:18789"
echo ""
echo "  4. (Optional) Start Ollama for local AI:"
echo "     docker compose --profile with-ollama up -d ollama"
echo "     docker exec greenforge-ollama ollama pull codestral"
echo ""
echo "Documentation: greenforge help"
echo ""
