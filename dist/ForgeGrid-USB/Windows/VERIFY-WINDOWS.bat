@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
set "PS_SCRIPT=%SCRIPT_DIR%VERIFY-WINDOWS.ps1"

powershell.exe -ExecutionPolicy Bypass -NoProfile -NonInteractive -File "%PS_SCRIPT%"
exit /b %ERRORLEVEL%
