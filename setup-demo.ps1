#!/usr/bin/env pwsh
# uMailServer Demo Setup Script (PowerShell)
# Usage: .\setup-demo.ps1

$ErrorActionPreference = "Stop"

Write-Host "=== uMailServer Demo Setup ===" -ForegroundColor Cyan
Write-Host ""

# Kill any existing server
Write-Host "[1/5] Cleaning previous data..." -ForegroundColor Yellow
Get-Process umailserver -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500
if (Test-Path data) { Remove-Item -Recurse -Force data }
New-Item -ItemType Directory -Path data | Out-Null

# Copy demo config
Write-Host "[2/5] Setting up demo configuration..." -ForegroundColor Yellow
Copy-Item demo.yaml umailserver.yaml -Force

# Create domain and accounts
Write-Host "[3/5] Creating domain and accounts..." -ForegroundColor Yellow
& .\umailserver.exe domain add localhost 2>$null

Write-Host "  Creating demo@localhost..." -ForegroundColor Gray
& .\umailserver.exe account add demo@localhost --password demo123 2>$null

Write-Host "  Creating alice@localhost..." -ForegroundColor Gray
& .\umailserver.exe account add alice@localhost --password alice123 2>$null

Write-Host "  Creating bob@localhost..." -ForegroundColor Gray
& .\umailserver.exe account add bob@localhost --password bob123 2>$null

# Start server
Write-Host "[4/5] Starting server..." -ForegroundColor Yellow
Start-Process -FilePath ".\umailserver.exe" -ArgumentList "serve --config demo.yaml" -WindowStyle Hidden
Start-Sleep -Seconds 3

# Send demo emails
Write-Host "[5/5] Sending demo emails..." -ForegroundColor Yellow
& .\umailclient.exe send --host localhost --port 25 --from noreply@github.com --to demo@localhost --subject "Welcome to uMailServer Demo!" --body "This is a demo email sent to your inbox. You can use this demo to test receiving and reading emails in webmail."

& .\umailclient.exe send --host localhost --port 25 --from alice@localhost --to demo@localhost --subject "Meeting Tomorrow" --body "Hi Demo, Can we schedule a meeting for tomorrow at 10 AM?"

& .\umailclient.exe send --host localhost --port 25 --from bob@localhost --to demo@localhost --subject "Project Update" --body "Demo, Here's the latest project update with all the new features."

Write-Host ""
Write-Host "============================================" -ForegroundColor Green
Write-Host "  uMailServer Demo is Ready!" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "Access:" -ForegroundColor White
Write-Host "  Webmail:  http://localhost:8081" -ForegroundColor Cyan
Write-Host "  Admin:    http://localhost:8082" -ForegroundColor Cyan
Write-Host ""
Write-Host "Login Credentials:" -ForegroundColor White
Write-Host "  Email:    demo@localhost" -ForegroundColor Green
Write-Host "  Password: demo123" -ForegroundColor Green
Write-Host ""
Write-Host "Demo Accounts:" -ForegroundColor White
Write-Host "  demo@localhost / demo123" -ForegroundColor Gray
Write-Host "  alice@localhost / alice123" -ForegroundColor Gray
Write-Host "  bob@localhost / bob123" -ForegroundColor Gray
Write-Host ""

# Open browser
Write-Host "Opening webmail..." -ForegroundColor Yellow
Start-Process "http://localhost:8081"

Read-Host "Press Enter to exit"
