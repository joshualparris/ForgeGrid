@echo off
set PORT=48192
echo Verifying ForgeGrid Windows Runtime...

echo [Stage 1] Checking executable existence...
if not exist "dist\ForgeGrid-USB\Windows\ForgeGrid.exe" (
    echo FAILED at Stage 1: dist\ForgeGrid-USB\Windows\ForgeGrid.exe not found
    exit /b 1
)

echo [Stage 2] Starting ForgeGrid Coordinator...
start /B dist\ForgeGrid-USB\Windows\ForgeGrid.exe -mode coordinator -port %PORT% -insecure > nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo FAILED at Stage 2: Failed to launch ForgeGrid.exe
    exit /b 1
)

echo [Stage 3] Waiting for server to initialize...
powershell -Command "Start-Sleep -Seconds 3"
if %ERRORLEVEL% neq 0 (
    echo FAILED at Stage 3: Sleep command failed
    taskkill /F /IM ForgeGrid.exe > nul 2>&1
    exit /b 1
)

echo [Stage 4] Testing Coordinator HTTP Status...
powershell -Command "try { $response = Invoke-WebRequest -Uri 'http://127.0.0.1:%PORT%/api/coordinator/status' -UseBasicParsing -ErrorAction Stop; if ($response.StatusCode -eq 200) { exit 0 } else { exit 1 } } catch { exit 1 }"
set RESULT=%ERRORLEVEL%

echo [Stage 5] Cleaning up...
taskkill /F /IM ForgeGrid.exe > nul 2>&1

if %RESULT% equ 0 (
    echo SUCCESS: Windows runtime verified successfully.
    exit /b 0
) else (
    echo FAILED at Stage 4: Coordinator HTTP Status check returned non-zero
    exit /b 1
)
