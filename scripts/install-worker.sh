#!/bin/bash
set -e

# docker-migrate worker installation script
# Usage: curl -sSL https://raw.githubusercontent.com/Altacee/dockation/main/scripts/install-worker.sh | bash -s -- \
#   --master-url https://master:9090 --token ABC123

# Default values
MASTER_URL=""
TOKEN=""
WORKER_NAME=$(hostname)
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/docker-migrate"
SERVICE_USER="root"
SKIP_DOCKER_CHECK=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --master-url)
            MASTER_URL="$2"
            shift 2
            ;;
        --token)
            TOKEN="$2"
            shift 2
            ;;
        --name)
            WORKER_NAME="$2"
            shift 2
            ;;
        --install-dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --skip-docker-check)
            SKIP_DOCKER_CHECK=true
            shift
            ;;
        --help)
            echo "Usage: $0 --master-url URL --token TOKEN [--name NAME]"
            echo ""
            echo "Options:"
            echo "  --master-url        Master gRPC URL (required)"
            echo "  --token             Enrollment token from master (required)"
            echo "  --name              Worker name (default: hostname)"
            echo "  --install-dir       Binary installation directory (default: /usr/local/bin)"
            echo "  --skip-docker-check Skip Docker installation check"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Validate required arguments
if [ -z "$MASTER_URL" ]; then
    log_error "--master-url is required"
    exit 1
fi

if [ -z "$TOKEN" ]; then
    log_error "--token is required"
    exit 1
fi

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        log_error "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

log_info "Detected OS: $OS, Architecture: $ARCH"

# Only Linux is supported for service installation
if [ "$OS" != "linux" ]; then
    log_error "Service installation is only supported on Linux"
    log_info "For other platforms, download the binary manually and run:"
    log_info "  docker-migrate worker --master-url $MASTER_URL --token $TOKEN"
    exit 1
fi

# Check for root
if [ "$EUID" -ne 0 ]; then
    log_error "This script must be run as root (use sudo)"
    exit 1
fi

# Check if Docker is installed
install_docker() {
    log_info "Installing Docker..."

    # Detect package manager and install Docker
    if command -v apt-get &> /dev/null; then
        # Debian/Ubuntu
        apt-get update -qq
        apt-get install -y -qq ca-certificates curl gnupg lsb-release

        # Add Docker's official GPG key
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg 2>/dev/null || true
        chmod a+r /etc/apt/keyrings/docker.gpg

        # Set up repository
        DISTRO=$(lsb_release -is 2>/dev/null | tr '[:upper:]' '[:lower:]' || echo "ubuntu")
        CODENAME=$(lsb_release -cs 2>/dev/null || echo "jammy")

        echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${DISTRO} ${CODENAME} stable" | \
            tee /etc/apt/sources.list.d/docker.list > /dev/null

        apt-get update -qq
        apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    elif command -v yum &> /dev/null; then
        # RHEL/CentOS/Fedora
        yum install -y -q yum-utils
        yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        yum install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    elif command -v dnf &> /dev/null; then
        # Fedora
        dnf install -y -q dnf-plugins-core
        dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
        dnf install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    elif command -v pacman &> /dev/null; then
        # Arch Linux
        pacman -Sy --noconfirm docker

    elif command -v apk &> /dev/null; then
        # Alpine
        apk add --no-cache docker

    else
        log_error "Could not detect package manager. Please install Docker manually."
        log_info "Visit: https://docs.docker.com/engine/install/"
        exit 1
    fi

    # Start and enable Docker
    systemctl start docker 2>/dev/null || service docker start 2>/dev/null || true
    systemctl enable docker 2>/dev/null || true

    log_info "Docker installed successfully"
}

if [ "$SKIP_DOCKER_CHECK" = false ]; then
    if ! command -v docker &> /dev/null; then
        log_warn "Docker is not installed"
        read -p "Would you like to install Docker? [Y/n] " -n 1 -r REPLY
        echo
        REPLY=${REPLY:-Y}
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            install_docker
        else
            log_error "Docker is required for docker-migrate worker"
            log_info "Install Docker manually or run with --skip-docker-check to skip this check"
            exit 1
        fi
    else
        log_info "Docker is installed"

        # Check if Docker daemon is running
        if ! docker info &> /dev/null; then
            log_warn "Docker daemon is not running, attempting to start..."
            systemctl start docker 2>/dev/null || service docker start 2>/dev/null || true
            sleep 2

            if ! docker info &> /dev/null; then
                log_warn "Could not start Docker daemon. The worker service may fail to start."
                log_info "Start Docker manually: systemctl start docker"
            else
                log_info "Docker daemon started"
            fi
        else
            log_info "Docker daemon is running"
        fi
    fi
fi

# Install git if not present
if ! command -v git &> /dev/null; then
    log_info "Installing git..."
    if command -v apt-get &> /dev/null; then
        apt-get update -qq && apt-get install -y -qq git
    elif command -v yum &> /dev/null; then
        yum install -y -q git
    elif command -v dnf &> /dev/null; then
        dnf install -y -q git
    elif command -v pacman &> /dev/null; then
        pacman -Sy --noconfirm git
    elif command -v apk &> /dev/null; then
        apk add --no-cache git
    else
        log_error "Could not install git. Please install it manually."
        exit 1
    fi
fi

# Install Go if not present
install_go() {
    log_info "Installing Go..."
    GO_VERSION="1.22.0"

    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz

    export PATH=$PATH:/usr/local/go/bin
    log_info "Go ${GO_VERSION} installed"
}

# Check for Go
if ! command -v go &> /dev/null; then
    if [ ! -f /usr/local/go/bin/go ]; then
        install_go
    else
        export PATH=$PATH:/usr/local/go/bin
    fi
fi

# Set up build directory
BUILD_DIR="/tmp/docker-migrate-build"
REPO_URL="https://github.com/Altacee/dockation.git"
BINARY_PATH="${INSTALL_DIR}/docker-migrate"

# Clone or update repository
if [ -d "$BUILD_DIR" ]; then
    log_info "Updating existing source..."
    cd "$BUILD_DIR"
    git fetch origin main
    git reset --hard origin/main
else
    log_info "Cloning repository from GitHub..."
    git clone "$REPO_URL" "$BUILD_DIR"
    cd "$BUILD_DIR"
fi

# Get current commit
COMMIT=$(git rev-parse --short HEAD)
log_info "Building from commit: $COMMIT"

# Build binary
log_info "Building docker-migrate..."
export PATH=$PATH:/usr/local/go/bin
export GOPROXY=https://proxy.golang.org,direct

cd "$BUILD_DIR"
go build -o "$BINARY_PATH" ./cmd/docker-migrate

chmod +x "$BINARY_PATH"
log_info "Binary installed to $BINARY_PATH"

# Verify binary works
if ! "$BINARY_PATH" --help > /dev/null 2>&1; then
    log_error "Binary verification failed"
    rm -f "$BINARY_PATH"
    exit 1
fi

log_info "Binary verified successfully (commit: $COMMIT)"

# Create config directory
mkdir -p "$CONFIG_DIR"
chmod 700 "$CONFIG_DIR"

# Create config file (durations omitted - defaults will be used)
CONFIG_FILE="${CONFIG_DIR}/worker.json"
cat > "$CONFIG_FILE" << EOF
{
  "role": "worker",
  "http_addr": ":8080",
  "grpc_addr": ":9090",
  "log_level": "info",
  "worker": {
    "master_url": "${MASTER_URL}",
    "name": "${WORKER_NAME}",
    "labels": {}
  }
}
EOF

chmod 600 "$CONFIG_FILE"
log_info "Config written to $CONFIG_FILE"

# Determine Docker service dependency
DOCKER_REQUIRES=""
DOCKER_AFTER="network-online.target"

if systemctl list-unit-files docker.service &> /dev/null; then
    DOCKER_REQUIRES="Requires=docker.service"
    DOCKER_AFTER="network-online.target docker.service"
    log_info "Docker systemd service detected"
elif systemctl list-unit-files docker.socket &> /dev/null; then
    DOCKER_REQUIRES="Requires=docker.socket"
    DOCKER_AFTER="network-online.target docker.socket"
    log_info "Docker socket activation detected"
else
    log_warn "Docker systemd service not found - service will start without Docker dependency"
fi

# Create systemd service file
SERVICE_FILE="/etc/systemd/system/docker-migrate-worker.service"
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Docker Migrate Worker
Documentation=https://github.com/Altacee/dockation
After=${DOCKER_AFTER}
Wants=network-online.target
${DOCKER_REQUIRES}

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=${BINARY_PATH} worker --master-url ${MASTER_URL} --token ${TOKEN} --name ${WORKER_NAME} --config ${CONFIG_FILE}
Restart=always
RestartSec=10
StartLimitInterval=60
StartLimitBurst=3
StandardOutput=journal
StandardError=journal

# Environment
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
EOF

log_info "Systemd service created at $SERVICE_FILE"

# Reload systemd and start service
systemctl daemon-reload
systemctl enable docker-migrate-worker

# Try to start the service
log_info "Starting docker-migrate-worker service..."
if systemctl start docker-migrate-worker 2>&1; then
    sleep 2
    if systemctl is-active --quiet docker-migrate-worker; then
        log_info "docker-migrate-worker service started successfully!"
    else
        log_warn "Service started but may not be running. Checking status..."
        systemctl status docker-migrate-worker --no-pager || true
    fi
else
    log_warn "Service failed to start on first attempt. Checking logs..."
    journalctl -u docker-migrate-worker -n 10 --no-pager 2>/dev/null || true
fi

log_info ""
log_info "Useful commands:"
log_info "  Check status:  systemctl status docker-migrate-worker"
log_info "  View logs:     journalctl -u docker-migrate-worker -f"
log_info "  Stop service:  systemctl stop docker-migrate-worker"
log_info "  Start service: systemctl start docker-migrate-worker"
log_info "  Restart:       systemctl restart docker-migrate-worker"
log_info ""
log_info "Installation complete! Worker '${WORKER_NAME}' configured to connect to master at ${MASTER_URL}"
