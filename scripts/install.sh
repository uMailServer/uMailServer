#!/bin/bash
# uMailServer Installation Script
# Usage: curl -fsSL https://get.umailserver.com | sudo bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Variables
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
DATA_DIR="${DATA_DIR:-/var/lib/umailserver}"
CONFIG_DIR="${CONFIG_DIR:-/etc/umailserver}"
USER="${USER:-umail}"

# Architecture detection
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${RED}Unsupported architecture: $ARCH${NC}"
        exit 1
        ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# Functions
print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║                                                           ║"
    echo "║   uMailServer - One binary. Complete email.              ║"
    echo "║                                                           ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

check_dependencies() {
    local deps=("curl" "systemctl")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Required dependency not found: $dep"
            exit 1
        fi
    done
}

create_user() {
    if ! id "$USER" &>/dev/null; then
        log_info "Creating user: $USER"
        useradd -r -s /bin/false -d "$DATA_DIR" "$USER"
    else
        log_warn "User $USER already exists"
    fi
}

create_directories() {
    log_info "Creating directories..."
    mkdir -p "$DATA_DIR" "$CONFIG_DIR"
    chown "$USER:$USER" "$DATA_DIR"
    chmod 750 "$DATA_DIR"
}

download_binary() {
    log_info "Downloading uMailServer..."

    local download_url
    if [[ "$VERSION" == "latest" ]]; then
        download_url="https://github.com/umailserver/umailserver/releases/latest/download/umailserver-${OS}-${ARCH}"
    else
        download_url="https://github.com/umailserver/umailserver/releases/download/${VERSION}/umailserver-${OS}-${ARCH}"
    fi

    log_info "Downloading from: $download_url"

    if ! curl -fsSL -o "${INSTALL_DIR}/umailserver" "$download_url"; then
        log_error "Failed to download uMailServer"
        exit 1
    fi

    chmod +x "${INSTALL_DIR}/umailserver"
    log_info "Binary installed to: ${INSTALL_DIR}/umailserver"
}

create_systemd_service() {
    log_info "Creating systemd service..."

    cat > /etc/systemd/system/umailserver.service << 'EOF'
[Unit]
Description=uMailServer - One binary. Complete email.
Documentation=https://umailserver.com/docs
After=network.target

[Service]
Type=simple
User=umail
Group=umail
ExecStart=/usr/local/bin/umailserver serve --config /etc/umailserver/umailserver.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=umailserver

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/umailserver

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    log_info "Systemd service created"
}

generate_config() {
    if [[ ! -f "${CONFIG_DIR}/umailserver.yaml" ]]; then
        log_info "Generating default configuration..."

        cat > "${CONFIG_DIR}/umailserver.yaml" << EOF
server:
  hostname: mail.$(hostname -d 2>/dev/null || echo "example.com")
  data_dir: ${DATA_DIR}

tls:
  acme:
    enabled: true
    email: admin@$(hostname -d 2>/dev/null || echo "example.com")

smtp:
  inbound:
    port: 25
  submission:
    port: 587
  submission_tls:
    port: 465

imap:
  port: 993

admin:
  port: 8443
  bind: 127.0.0.1
EOF

        chmod 600 "${CONFIG_DIR}/umailserver.yaml"
        log_info "Configuration file created: ${CONFIG_DIR}/umailserver.yaml"
    else
        log_warn "Configuration file already exists"
    fi
}

open_ports() {
    log_info "Configuring firewall..."

    if command -v ufw &> /dev/null; then
        ufw allow 25/tcp
        ufw allow 587/tcp
        ufw allow 465/tcp
        ufw allow 993/tcp
        ufw allow 8443/tcp
        log_info "UFW rules added"
    elif command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port=25/tcp
        firewall-cmd --permanent --add-port=587/tcp
        firewall-cmd --permanent --add-port=465/tcp
        firewall-cmd --permanent --add-port=993/tcp
        firewall-cmd --permanent --add-port=8443/tcp
        firewall-cmd --reload
        log_info "FirewallD rules added"
    else
        log_warn "No supported firewall found (ufw or firewalld)"
    fi
}

print_next_steps() {
    echo -e "${GREEN}"
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║                                                           ║"
    echo "║   uMailServer installed successfully!                    ║"
    echo "║                                                           ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo ""
    echo "Next steps:"
    echo ""
    echo "  1. Edit configuration:"
    echo "     sudo nano ${CONFIG_DIR}/umailserver.yaml"
    echo ""
    echo "  2. Run quickstart to create your first account:"
    echo "     sudo umailserver quickstart admin@yourdomain.com"
    echo ""
    echo "  3. Start the server:"
    echo "     sudo systemctl enable --now umailserver"
    echo ""
    echo "  4. Check status:"
    echo "     sudo systemctl status umailserver"
    echo ""
    echo "  5. View logs:"
    echo "     sudo journalctl -u umailserver -f"
    echo ""
    echo "Documentation: https://docs.umailserver.com"
    echo "Support: https://github.com/umailserver/umailserver/issues"
    echo ""
}

# Main
main() {
    print_banner
    check_root
    check_dependencies

    log_info "Installing uMailServer ${VERSION} for ${OS}/${ARCH}..."

    create_user
    create_directories
    download_binary
    create_systemd_service
    generate_config
    open_ports

    print_next_steps
}

main "$@"
