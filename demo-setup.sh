#!/bin/bash
# uMailServer Demo Setup Script
# This script sets up a working demo environment

set -e

echo "=== uMailServer Demo Setup ==="
echo ""

# Clean previous data
echo "[1/5] Cleaning previous data..."
rm -rf ./data
mkdir -p ./data

# Copy demo config
echo "[2/5] Setting up demo configuration..."
cp demo.yaml umailserver.yaml

# Initialize database and demo account
echo "[3/5] Creating demo account..."
echo "demo@localhost" | ./umailserver.exe account add demo@localhost --password demo123

# Create additional demo accounts
./umailserver.exe account add alice@localhost --password alice123
./umailserver.exe account add bob@localhost --password bob123

# Create demo emails
echo "[4/5] Sending demo emails to inbox..."

# Send some test emails using SMTP
./umailclient.exe send --host localhost --port 25 \
  --from noreply@github.com \
  --to demo@localhost \
  --subject "Welcome to uMailServer Demo!" \
  --body "This is a demo email sent to your inbox.

You can use this demo to test:
- Receiving emails
- Reading emails in webmail
- Replying to emails
- And more!

Best regards,
uMailServer Demo System"

./umailclient.exe send --host localhost --port 25 \
  --from alice@localhost \
  --to demo@localhost \
  --subject "Meeting Tomorrow" \
  --body "Hi Demo,

Can we schedule a meeting for tomorrow at 10 AM?

Best,
Alice"

./umailclient.exe send --host localhost --port 25 \
  --from bob@localhost \
  --to demo@localhost \
  --subject "Project Update" \
  --body "Demo,

Here's the latest project update with all the new features.

Regards,
Bob"

echo "[5/5] Starting server..."
echo ""
echo "=== Demo Ready! ==="
echo ""
echo "Server will start on:"
echo "  - SMTP:     localhost:25"
echo "  - IMAP:     localhost:143"
echo "  - Webmail:  http://localhost:8081"
echo "  - Admin:    http://localhost:8082"
echo ""
echo "Demo Accounts:"
echo "  - demo@localhost / demo123"
echo "  - alice@localhost / alice123"
echo "  - bob@localhost / bob123"
echo ""
echo "Starting server in background..."
./umailserver.exe serve --config demo.yaml &
sleep 3
echo ""
echo "Server is running! Open http://localhost:8081 to access webmail."
