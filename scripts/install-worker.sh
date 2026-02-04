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
        --help)
            echo "Usage: $0 --master-url URL --token TOKEN [--name NAME]"
            echo ""
            echo "Options:"
            echo "  --master-url    Master gRPC URL (required)"
            echo "  --token         Enrollment token from master (required)"
            echo "  --name          Worker name (default: hostname)"
            echo "  --install-dir   Binary installation directory (default: /usr/local/bin)"
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
    log_error "This script must be run as root"
    exit 1
fi

# Get latest release version from GitHub
log_info "Fetching latest release..."
LATEST_VERSION=$(curl -sL https://api.github.com/repos/Altacee/dockation/releases/latest | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_VERSION" ]; then
    log_warn "Could not determine latest version, using 'latest'"
    LATEST_VERSION="latest"
fi

log_info "Installing version: $LATEST_VERSION"

# Download binary
DOWNLOAD_URL="https://github.com/Altacee/dockation/releases/download/${LATEST_VERSION}/docker-migrate-${OS}-${ARCH}"
BINARY_PATH="${INSTALL_DIR}/docker-migrate"

log_info "Downloading docker-migrate from ${DOWNLOAD_URL}..."
if ! curl -sL "$DOWNLOAD_URL" -o "$BINARY_PATH"; then
    log_error "Failed to download binary"
    exit 1
fi

chmod +x "$BINARY_PATH"
log_info "Binary installed to $BINARY_PATH"

# Verify binary works
if ! "$BINARY_PATH" --help > /dev/null 2>&1; then
    log_error "Binary verification failed"
    exit 1
fi

log_info "Binary verified successfully"

# Create config directory
mkdir -p "$CONFIG_DIR"
chmod 700 "$CONFIG_DIR"

# Create config file
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
    "labels": {},
    "reconnect_interval": "5s",
    "max_reconnect_interval": "5m"
  }
}
EOF

chmod 600 "$CONFIG_FILE"
log_info "Config written to $CONFIG_FILE"

# Create systemd service file
SERVICE_FILE="/etc/systemd/system/docker-migrate-worker.service"
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Docker Migrate Worker
Documentation=https://github.com/Altacee/dockation
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=${BINARY_PATH} worker --master-url ${MASTER_URL} --token ${TOKEN} --name ${WORKER_NAME} --config ${CONFIG_FILE}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=false
ProtectSystem=false
ProtectHome=false

# Environment
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
EOF

log_info "Systemd service created at $SERVICE_FILE"

# Reload systemd and start service
systemctl daemon-reload
systemctl enable docker-migrate-worker
systemctl start docker-migrate-worker

# Check status
sleep 2
if systemctl is-active --quiet docker-migrate-worker; then
    log_info "docker-migrate-worker service started successfully!"
    log_info ""
    log_info "Useful commands:"
    log_info "  Check status:  systemctl status docker-migrate-worker"
    log_info "  View logs:     journalctl -u docker-migrate-worker -f"
    log_info "  Stop service:  systemctl stop docker-migrate-worker"
    log_info "  Start service: systemctl start docker-migrate-worker"
else
    log_error "Service failed to start. Check logs with: journalctl -u docker-migrate-worker"
    exit 1
fi

log_info ""
log_info "Installation complete! Worker '${WORKER_NAME}' is connecting to master at ${MASTER_URL}"
