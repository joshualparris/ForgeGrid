@echo off
echo Starting ForgeGrid Coordinator...
ForgeGrid.exe -mode coordinator
if %ERRORLEVEL% neq 0 (
    echo ForgeGrid failed to start.
    pause
)
