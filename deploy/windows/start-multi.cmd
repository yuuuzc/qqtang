@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start-local.ps1" -Prebuilt -ClientCount 2 %*
if errorlevel 1 pause
endlocal
