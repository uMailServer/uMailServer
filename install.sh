#!/bin/bash
#
# uMailServer Installer
# Installs uMailServer on Linux with systemd service
#

set -e

INSTALL_DIR="/opt/umailserver"
DATA_DIR="/var/lib/umailserver"
CONFIG_DIR="/etc/umailserver"
LOG_DIR="/var/log/umailserver"
RUN_DIR="/var/run/umailserver"
BIN_NAME="umailserver"
USER="umailserver"
GROUP="umailserver"
SERVICE_FILE="/etc/systemd/system/umailserver.service"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -c, --config FILE      Use custom config file (default: interactive setup)
    -d, --data-dir DIR     Data directory (default: /var/lib/umailserver)
    -p, --port PORT        HTTP port for initial setup (default: 443)
    -n, --no-service       Skip systemd service installation
    -s, --skip-setup       Skip interactive setup wizard
    -y, --assume-yes      Automatic yes to prompts

Examples:
    $0                      # Interactive install
    $0 -c /path/to/config   # Install with existing config
    $0 -s                   # Install binary only, no service
EOF
    exit 0
}

# Parse arguments
CONFIG_FILE=""
NO_SERVICE=false
SKIP_SETUP=false
ASSUME_YES=false
CUSTOM_DATA_DIR=""
HTTP_PORT="443"

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help) usage ;;
        -c|--config) CONFIG_FILE="$2"; shift 2 ;;
        -d|--data-dir) CUSTOM_DATA_DIR="$2"; shift 2 ;;
        -p|--port) HTTP_PORT="$2"; shift 2 ;;
        -n|--no-service) NO_SERVICE=true; shift ;;
        -s|--skip-setup) SKIP_SETUP=true; shift ;;
        -y|--assume-yes) ASSUME_YES=true; shift ;;
        *) error "Unknown option: $1"; exit 1 ;;
    esac
done

# Check if running as root
if [[ $EUID -ne 0 ]] && [[ "$ASSUME_YES" == "false" ]]; then
    warn "Running as non-root. Some operations may fail."
    warn "Run with sudo or as root for full installation."
    echo ""
    read -p "Continue anyway? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Detect init system
detect_init() {
    if command -v systemctl &> /dev/null && systemctl --user &> /dev/null 2>&1; then
        echo "systemd"
    elif command -v systemctl &> /dev/null; then
        echo "systemd"
    elif command -v initctl &> /dev/null; then
        echo "upstart"
    else
        echo "none"
    fi
}

# Check for Go (for building)
check_go() {
    if command -v go &> /dev/null; then
        GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
        info "Found Go $GO_VERSION"
    else
        warn "Go not found. You can build manually or download pre-built binary."
    fi
}

# Create user and group
create_user() {
    if id "$USER" &>/dev/null; then
        info "User $USER already exists"
    else
        info "Creating user $USER..."
        useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin --no-create-home "$USER" 2>/dev/null || \
        useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin --no-create-home "$USER" 2>/dev/null || \
        warn "Could not create user $USER. Running as root."
    fi
}

# Create directories
create_directories() {
    local dirs=("$DATA_DIR" "$CONFIG_DIR" "$LOG_DIR" "$RUN_DIR" "$INSTALL_DIR")
    for dir in "${dirs[@]}"; do
        if [[ -d "$dir" ]]; then
            info "Directory exists: $dir"
        else
            info "Creating directory: $dir"
            mkdir -p "$dir"
            chmod 755 "$dir"
        fi
    done
}

# Set ownership
set_ownership() {
    if id "$USER" &>/dev/null 2>/dev/null; then
        info "Setting ownership to $USER:$GROUP"
        chown -R "$USER:$GROUP" "$DATA_DIR" "$LOG_DIR" "$RUN_DIR" 2>/dev/null || true
        chown -R root:root "$CONFIG_DIR" 2>/dev/null || true
    fi
}

# Build binary
build_binary() {
    info "Building uMailServer..."

    # Detect architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH_DIR="linux-amd64" ;;
        aarch64) ARCH_DIR="linux-arm64" ;;
        armv7l) ARCH_DIR="linux-armv7" ;;
        *) ARCH_DIR="linux-amd64" ;;
    esac

    # Try to build
    if command -v go &> /dev/null; then
        cd "$(dirname "$0")"
        CGO_ENABLED=0 go build -ldflags "-s -w" -o "$INSTALL_DIR/$BIN_NAME" ./cmd/umailserver
        chmod +x "$INSTALL_DIR/$BIN_NAME"
        info "Binary built successfully: $INSTALL_DIR/$BIN_NAME"
    else
        error "Go not installed. Please install Go 1.25+ or download pre-built binary."
        error "Download: https://github.com/uMailServer/uMailServer/releases"
        exit 1
    fi
}

# Download pre-built binary
download_binary() {
    local version="${1:-latest}"
    local arch=$(uname -m)
    case $arch in
        x86_64) arch="amd64" ;;
        aarch64) arch="arm64" ;;
        arm*) arch="armv7" ;;
    esac

    local url="https://github.com/uMailServer/uMailServer/releases/download/v${version}/umailserver-${arch}.tar.gz"

    info "Downloading from $url..."
    if command -v curl &> /dev/null; then
        curl -L "$url" -o "/tmp/umailserver.tar.gz"
    elif command -v wget &> /dev/null; then
        wget -O "/tmp/umailserver.tar.gz" "$url"
    else
        error "Neither curl nor wget found. Please download manually."
        exit 1
    fi

    tar -xzf "/tmp/umailserver.tar.gz" -C "$INSTALL_DIR"
    chmod +x "$INSTALL_DIR/$BIN_NAME"
    rm -f "/tmp/umailserver.tar.gz"
    info "Binary installed: $INSTALL_DIR/$BIN_NAME"
}

# Install systemd service
install_systemd_service() {
    info "Installing systemd service..."

    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=uMailServer - Email Server
Documentation=https://github.com/uMailServer/uMailServer
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$USER
Group=$GROUP
WorkingDirectory=$DATA_DIR
ExecStart=$INSTALL_DIR/$BIN_NAME serve --config $CONFIG_DIR/umailserver.yaml
Restart=always
RestartSec=5
TimeoutStopSec=30
LimitNOFILE=65536

# Environment
Environment=HOME=$DATA_DIR

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=umailserver

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=$DATA_DIR $LOG_DIR $RUN_DIR

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "$SERVICE_FILE"
    systemctl daemon-reload
    info "Systemd service installed: $SERVICE_FILE"
}

# Interactive setup
run_setup() {
    info "Starting interactive setup..."

    local hostname=""
    local admin_email=""
    local domain=""

    echo ""
    echo "=== uMailServer Setup ==="
    echo ""

    # Hostname
    while [[ -z "$hostname" ]]; do
        read -p "Server hostname [mail.example.com]: " hostname
        hostname="${hostname:-mail.example.com}"
    done

    # Admin email
    while [[ -z "$admin_email" ]]; do
        read -p "Admin email [admin@${hostname#*.}]: " admin_email
        admin_email="${admin_email:-admin@${hostname#*.}}"
    done

    # Primary domain
    while [[ -z "$domain" ]]; do
        read -p "Primary email domain [${hostname#*.}]: " domain
        domain="${domain:-${hostname#*.}}"
    done

    # Generate JWT secret
    JWT_SECRET=$(head -c 32 /dev/urandom | base64 | head -c 32)

    # Create config
    info "Creating configuration..."
    cat > "$CONFIG_DIR/umailserver.yaml" <<EOF
# uMailServer Configuration
# Generated by install.sh on $(date)

server:
  hostname: $hostname
  data_dir: $DATA_DIR

tls:
  cert_file: ""
  key_file: ""
  min_version: "1.2"
  acme:
    enabled: true
    email: $admin_email
    provider: letsencrypt
    challenge: http-01

smtp:
  inbound:
    enabled: true
    port: 25
    bind: "0.0.0.0"
    max_message_size: 50MB
    max_recipients: 100
    max_connections: 10000
  submission:
    enabled: true
    port: 587
    bind: "0.0.0.0"
    require_auth: true
    require_tls: true
  submission_tls:
    enabled: true
    port: 465
    bind: "0.0.0.0"
    require_auth: true

imap:
  enabled: true
  port: 993
  bind: "0.0.0.0"
  starttls_port: 143

pop3:
  enabled: false
  port: 995
  bind: "0.0.0.0"

http:
  enabled: true
  port: 443
  http_port: 80
  bind: "0.0.0.0"
  cors_origins: []

admin:
  enabled: true
  bind: "127.0.0.1"
  port: 8443

security:
  max_login_attempts: 5
  lockout_duration: 15m
  jwt_secret: "$JWT_SECRET"
  rate_limit:
    smtp_per_minute: 30
    smtp_per_hour: 500
    imap_connections: 50
    http_requests_per_minute: 120

spam:
  enabled: true
  reject_threshold: 9.0
  junk_threshold: 3.0
  quarantine_threshold: 6.0
  bayesian:
    enabled: true
    auto_train: true
  greylisting:
    enabled: true
    delay: 5m
  rbl_servers:
    - zen.spamhaus.org
    - b.barracudacentral.org

av:
  enabled: false
  addr: "127.0.0.1:3310"

mcp:
  enabled: true
  port: 3000
  bind: "127.0.0.1"

logging:
  level: info
  format: json
  output: stdout

metrics:
  enabled: true
  port: 8080
  bind: "127.0.0.1"

database:
  path: $DATA_DIR/umailserver.db

storage:
  sync: true

domains:
  - name: $domain
    max_accounts: 100
    max_mailbox_size: 5GB
EOF

    chmod 600 "$CONFIG_DIR/umailserver.yaml"
    info "Configuration created: $CONFIG_DIR/umailserver.yaml"

    # Initialize database
    info "Initializing database..."
    if [[ -x "$INSTALL_DIR/$BIN_NAME" ]]; then
        "$INSTALL_DIR/$BIN_NAME" db migrate --config "$CONFIG_DIR/umailserver.yaml" 2>/dev/null || true
    fi
}

# Main installation
main() {
    info "uMailServer Installer"
    echo ""

    check_go
    create_directories

    if [[ "$NO_SERVICE" == "false" ]]; then
        create_user
        set_ownership
    fi

    # Check if binary exists in current directory
    if [[ -x "./$BIN_NAME" ]]; then
        info "Using existing binary in current directory"
        cp "./$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
        chmod +x "$INSTALL_DIR/$BIN_NAME"
    elif [[ -x "$INSTALL_DIR/$BIN_NAME" ]]; then
        info "Binary already exists: $INSTALL_DIR/$BIN_NAME"
    else
        if command -v go &> /dev/null; then
            build_binary
        else
            download_binary
        fi
    fi

    if [[ "$NO_SERVICE" == "false" ]]; then
        local init_system=$(detect_init)
        if [[ "$init_system" == "systemd" ]]; then
            install_systemd_service
        else
            warn "Systemd not found. Skipping service installation."
            warn "To run manually: $INSTALL_DIR/$BIN_NAME serve --config $CONFIG_DIR/umailserver.yaml"
        fi
    fi

    if [[ "$SKIP_SETUP" == "false" && -z "$CONFIG_FILE" ]]; then
        run_setup
    elif [[ -n "$CONFIG_FILE" && -f "$CONFIG_FILE" ]]; then
        cp "$CONFIG_FILE" "$CONFIG_DIR/umailserver.yaml"
        chmod 600 "$CONFIG_DIR/umailserver.yaml"
        info "Configuration installed: $CONFIG_DIR/umailserver.yaml"
    fi

    echo ""
    info "Installation complete!"
    echo ""
    echo "Next steps:"
    echo "  1. Edit config: $CONFIG_DIR/umailserver.yaml"
    echo "  2. Start service: systemctl start umailserver"
    echo "  3. Enable on boot: systemctl enable umailserver"
    echo "  4. Check status: systemctl status umailserver"
    echo ""
    echo "Ports:"
    echo "  SMTP:     25, 587, 465"
    echo "  IMAP:     143, 993"
    echo "  POP3:     995"
    echo "  HTTP:     443 (Webmail + Admin)"
    echo "  Admin:    8443 (localhost only)"
    echo "  Metrics:  8080"
    echo "  MCP:      3000"
    echo ""
}

main
