@echo off
REM uMailServer Demo Setup Script
REM Usage: setup-demo.bat

echo === uMailServer Demo Setup ===
echo.

REM Kill any existing server
taskkill /f /im umailserver.exe 2>nul

REM Clean previous data
echo [1/5] Cleaning previous data...
if exist data rmdir /s /q data
mkdir data

REM Copy demo config
echo [2/5] Setting up demo configuration...
copy demo.yaml umailserver.yaml >nul

REM Create domain
echo [3/5] Creating domain and accounts...
.\umailserver.exe domain add localhost >nul 2>&1

REM Create demo accounts with password
echo Creating demo@localhost...
.\umailserver.exe account add demo@localhost --password demo1234

echo Creating alice@localhost...
.\umailserver.exe account add alice@localhost --password alice1234

echo Creating bob@localhost...
.\umailserver.exe account add bob@localhost --password bob12345

REM Start server in background
echo [4/5] Starting server...
start "" .\umailserver.exe serve --config demo.yaml

REM Wait for server to start (using ping as delay)
echo Waiting for server to start...
ping -n 5 localhost >nul 2>&1

REM Send demo emails
echo [5/5] Sending demo emails...
.\umailclient.exe send --host localhost --port 25 --from noreply@github.com --to demo@localhost --subject "Welcome to uMailServer Demo!" --body "This is a demo email. You can use this demo to test the webmail interface."
.\umailclient.exe send --host localhost --port 25 --from alice@localhost --to demo@localhost --subject "Meeting Tomorrow" --body "Hi Demo, Can we schedule a meeting for tomorrow at 10 AM? - Alice"
.\umailclient.exe send --host localhost --port 25 --from bob@localhost --to demo@localhost --subject "Project Update" --body "Demo, Here's the latest project update. - Bob"

echo.
echo ============================================
echo   uMailServer Demo is Ready!
echo ============================================
echo.
echo Access:
echo   Webmail:  http://localhost:8082/webmail/
echo   Admin:    http://localhost:8082/
echo.
echo Login Credentials:
echo   Email:    demo@localhost
echo   Password: demo1234
echo.
echo Demo Accounts:
echo   demo@localhost / demo1234
echo   alice@localhost / alice1234
echo   bob@localhost / bob12345
echo.
echo Opening webmail...
start http://localhost:8082/webmail/
pause
