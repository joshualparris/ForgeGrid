@echo off
set PORT=48192
echo Verifying ForgeGrid Windows Runtime...

start /B dist\ForgeGrid-USB\Windows\ForgeGrid.exe -mode coordinator -port %PORT% -insecure > nul 2>&1

timeout /t 3 > nul

powershell -Command "$response = Invoke-WebRequest -Uri 'http://127.0.0.1:%PORT%/api/coordinator/status' -UseBasicParsing -ErrorAction Ignore; if ($response.StatusCode -eq 200 -or $response.StatusCode -eq 401) { exit 0 } else { exit 1 }"
set RESULT=%ERRORLEVEL%

taskkill /F /IM ForgeGrid.exe > nul 2>&1

if %RESULT% equ 0 (
    echo Windows runtime verified successfully.
    exit /b 0
) else (
    echo Windows runtime verification failed.
    exit /b 1
)
