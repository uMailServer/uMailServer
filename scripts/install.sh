#!/bin/bash
set -e

# uMailServer Installation Script
# Supports: Linux AMD64/ARM64

REPO="uMailServer/uMailServer"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/umailserver"
DATA_DIR="/var/lib/umailserver"
SERVICE_USER="umail"

# Colors
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

# Detect architecture
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "linux-amd64"
            ;;
        aarch64|arm64)
            echo "linux-arm64"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# Get latest release version
get_latest_version() {
    curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
}

# Download binary
download_binary() {
    local version=$1
    local arch=$2
    local url="https://github.com/$REPO/releases/download/$version/umailserver-$arch"

    log_info "Downloading uMailServer $version for $arch..."

    if ! curl -sL "$url" -o /tmp/umailserver; then
        log_error "Failed to download binary"
        exit 1
    fi

    chmod +x /tmp/umailserver
}

# Create user and directories
setup_directories() {
    log_info "Setting up directories..."

    # Create service user
    if ! id "$SERVICE_USER" &>/dev/null; then
        useradd -r -s /bin/false -d "$DATA_DIR" "$SERVICE_USER"
        log_info "Created user: $SERVICE_USER"
    fi

    # Create directories
    mkdir -p "$CONFIG_DIR" "$DATA_DIR" "/var/spool/umailserver"
    chown -R "$SERVICE_USER:$SERVICE_USER" "$DATA_DIR" "/var/spool/umailserver"

    log_info "Directories created"
}

# Install systemd service
install_service() {
    log_info "Installing systemd service..."

    cat > /etc/systemd/system/umailserver.service << 'EOF'
[Unit]
Description=uMailServer - Unified Mail Server
After=network.target

[Service]
Type=simple
User=umail
Group=umail
ExecStart=/usr/local/bin/umailserver serve --config /etc/umailserver/config.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    log_info "Systemd service installed"
}

# Create default config
create_config() {
    if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
        log_info "Creating default configuration..."

        cat > "$CONFIG_DIR/config.yaml" << EOF
server:
  hostname: $(hostname -f 2>/dev/null || echo "mail.example.com")
  data_dir: $DATA_DIR
  tls:
    cert: $CONFIG_DIR/certs/cert.pem
    key: $CONFIG_DIR/certs/key.pem

smtp:
  inbound:
    bind: 0.0.0.0
    port: 25
    max_message_size: 52428800  # 50MB
    max_recipients: 100
  outbound:
    bind: 0.0.0.0
    port: 587

imap:
  bind: 0.0.0.0
  port: 143
  tls_port: 993

api:
  bind: 0.0.0.0
  port: 8080
  jwt_secret: $(openssl rand -hex 32)

logging:
  level: info
  format: json
EOF

        log_info "Default config created at $CONFIG_DIR/config.yaml"
        log_warn "Please edit the configuration file before starting the service"
    fi
}

# Main installation
main() {
    log_info "uMailServer Installer"
    log_info "====================="

    # Check if running as root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi

    # Detect architecture
    ARCH=$(detect_arch)
    log_info "Detected architecture: $ARCH"

    # Get latest version
    VERSION=$(get_latest_version)
    if [[ -z "$VERSION" ]]; then
        log_error "Could not determine latest version"
        exit 1
    fi
    log_info "Latest version: $VERSION"

    # Download binary
    download_binary "$VERSION" "$ARCH"

    # Setup directories
    setup_directories

    # Install binary
    mv /tmp/umailserver "$INSTALL_DIR/umailserver"
    log_info "Binary installed to $INSTALL_DIR/umailserver"

    # Create config
    create_config

    # Install service
    install_service

    log_info ""
    log_info "Installation complete!"
    log_info ""
    log_info "Next steps:"
    log_info "  1. Edit configuration: sudo nano $CONFIG_DIR/config.yaml"
    log_info "  2. Setup TLS certificates in $CONFIG_DIR/certs/"
    log_info "  3. Create admin account: sudo umailserver account create admin@yourdomain.com"
    log_info "  4. Start service: sudo systemctl start umailserver"
    log_info "  5. Enable service: sudo systemctl enable umailserver"
    log_info ""
    log_info "Web interface will be available at: http://your-server:8080"
}

main "$@"
