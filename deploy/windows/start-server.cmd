@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start-server.ps1" -Prebuilt %*
if errorlevel 1 pause
endlocal
