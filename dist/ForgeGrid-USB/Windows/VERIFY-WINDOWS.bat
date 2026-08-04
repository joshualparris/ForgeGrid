@echo off
setlocal EnableDelayedExpansion

set "SCRIPT_DIR=%~dp0"
set "EXE=%SCRIPT_DIR%ForgeGrid.exe"
set "PORT=48192"
set "PID_FILE=%TEMP%\ForgeGrid-Test-%RANDOM%.pid"
set "EXIT_CODE=0"

echo Verifying ForgeGrid Windows Runtime...

echo [Stage 1] Checking executable existence...
if not exist "%EXE%" (
    echo FAILED at Stage 1: "%EXE%" not found
    exit /b 1
)

echo [Stage 2] Starting isolated ForgeGrid Coordinator...
powershell -Command "$p = Start-Process -FilePath '%EXE%' -ArgumentList '-mode coordinator -port %PORT%' -PassThru -WindowStyle Hidden; if ($p -and $p.Id) { Set-Content -Path '%PID_FILE%' -Value $p.Id; exit 0 } else { exit 1 }"
if %ERRORLEVEL% neq 0 (
    echo FAILED at Stage 2: Failed to launch ForgeGrid.exe
    exit /b 1
)

if not exist "%PID_FILE%" (
    echo FAILED at Stage 2: PID file was not created
    exit /b 1
)

set /p PID=<%PID_FILE%
echo Captured ForgeGrid PID: %PID%

echo [Stage 3] Waiting for HTTPS server to initialize...
set "READY=0"
for /L %%i in (1,1,10) do (
    curl.exe -k -sS -f https://127.0.0.1:%PORT%/api/coordinator/status > nul 2>&1
    if !ERRORLEVEL! equ 0 (
        set "READY=1"
        goto :ready
    )

    :: Check if process died early
    tasklist /FI "PID eq %PID%" | find "%PID%" > nul
    if !ERRORLEVEL! neq 0 (
        echo FAILED at Stage 3: Process %PID% exited prematurely before becoming ready.
        set "EXIT_CODE=1"
        goto :cleanup
    )

    powershell -Command "Start-Sleep -Seconds 1"
)

:ready
if "%READY%"=="0" (
    echo FAILED at Stage 3: Coordinator HTTPS Status check timed out or failed
    set "EXIT_CODE=1"
) else (
    echo SUCCESS: Windows runtime HTTPS endpoint verified successfully.
    set "EXIT_CODE=0"
)

:cleanup
echo [Stage 4] Cleaning up isolated test process...
if defined PID (
    tasklist /FI "PID eq %PID%" | find "%PID%" > nul
    if !ERRORLEVEL! equ 0 (
        taskkill /PID %PID% /F > nul 2>&1
        if !ERRORLEVEL! neq 0 (
            echo FAILED: Cleanup could not safely terminate process %PID%
            set "EXIT_CODE=1"
        )
    )
)

if exist "%PID_FILE%" (
    del "%PID_FILE%" > nul 2>&1
    if exist "%PID_FILE%" (
        echo FAILED: Could not remove temporary PID file %PID_FILE%
        set "EXIT_CODE=1"
    )
)

exit /b %EXIT_CODE%
