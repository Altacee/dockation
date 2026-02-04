#!/bin/bash
# install-service.sh - Install docker-migrate as a systemd service
# This script must be run with root privileges

set -e

BINARY_PATH="/usr/local/bin/docker-migrate"
SERVICE_FILE="/etc/systemd/system/docker-migrate.service"
DATA_DIR="/var/lib/docker-migrate"
CONFIG_DIR="/etc/docker-migrate"
CONFIG_FILE="$CONFIG_DIR/config.yaml"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Error: This script must be run as root"
    echo "Usage: sudo $0"
    exit 1
fi

echo "Installing docker-migrate systemd service..."

# Create directories
echo "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"

# Copy binary
if [ -f "bin/docker-migrate" ]; then
    echo "Installing binary to $BINARY_PATH..."
    cp bin/docker-migrate "$BINARY_PATH"
    chmod +x "$BINARY_PATH"
else
    echo "Error: Binary not found at bin/docker-migrate"
    echo "Please run 'make build' first"
    exit 1
fi

# Create default config if not exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Creating default configuration at $CONFIG_FILE..."
    cat > "$CONFIG_FILE" << 'EOF'
# Docker Migration Tool Configuration

# Server addresses
http_addr: ":8080"
grpc_addr: ":9090"

# Data directory for state and transfers
data_dir: "/var/lib/docker-migrate"

# Docker daemon socket (leave empty for default)
docker_host: ""

# Logging
log_level: "info"      # debug, info, warn, error
log_format: "json"     # json or console

# Security: Patterns to redact in logs
redact_patterns:
  - "PASSWORD"
  - "SECRET"
  - "KEY"
  - "TOKEN"
  - "CREDENTIAL"
  - "API_KEY"
  - "PRIVATE"

# Transfer settings
transfer:
  chunk_size: 1048576        # 1MB chunks
  max_concurrent: 4          # Parallel transfers
  compression: true          # Enable compression
  checksum_algorithm: "sha256"

# Peer discovery
discovery:
  enabled: true
  mdns_enabled: true
  mdns_service_name: "_docker-migrate._tcp"

# Rate limiting for pairing attempts
rate_limit:
  max_attempts: 5
  window_minutes: 15
  ban_duration_minutes: 30

# Migration defaults
migration:
  default_strategy: "cold"   # cold, warm, or snapshot
  default_mode: "copy"       # copy or move
  checkpoint_interval: 30    # seconds
  verify_checksums: true

# Health check intervals
health:
  interval_seconds: 10
  timeout_seconds: 5
EOF
    echo "Default configuration created"
else
    echo "Configuration already exists at $CONFIG_FILE"
fi

# Create systemd service file
echo "Creating systemd service file at $SERVICE_FILE..."
cat > "$SERVICE_FILE" << 'EOF'
[Unit]
Description=Docker Migration Tool
Documentation=https://github.com/artemis/docker-migrate
After=network.target docker.service
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
User=root
Group=docker

# Start the service
ExecStart=/usr/local/bin/docker-migrate serve --config /etc/docker-migrate/config.yaml

# Restart behavior
Restart=always
RestartSec=5
StartLimitInterval=0

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/docker-migrate
PrivateTmp=true
ProtectHome=true
ProtectKernelTunables=true
ProtectControlGroups=true

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=docker-migrate

[Install]
WantedBy=multi-user.target
EOF

# Set proper permissions
chmod 644 "$SERVICE_FILE"
chmod 755 "$DATA_DIR"
chmod 644 "$CONFIG_FILE"

# Reload systemd daemon
echo "Reloading systemd daemon..."
systemctl daemon-reload

# Enable service
echo "Enabling docker-migrate service..."
systemctl enable docker-migrate

echo ""
echo "Installation complete!"
echo ""
echo "Configuration file: $CONFIG_FILE"
echo "Data directory: $DATA_DIR"
echo "Binary location: $BINARY_PATH"
echo ""
echo "To start the service:"
echo "  sudo systemctl start docker-migrate"
echo ""
echo "To check status:"
echo "  sudo systemctl status docker-migrate"
echo ""
echo "To view logs:"
echo "  sudo journalctl -u docker-migrate -f"
echo ""
echo "To stop the service:"
echo "  sudo systemctl stop docker-migrate"
echo ""
