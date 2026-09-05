@echo off
setlocal
cd /d "%~dp0"

set "SERVER=runtime\bin\qqt-server-local.exe"
set "CONFIG=configs\server-directory-local-ui.json"

if not exist "%SERVER%" (
  echo Missing server executable: %SERVER%
  echo Please extract the complete QQTang-Local release package.
  exit /b 1
)
if not exist "%CONFIG%" (
  echo Missing server configuration: %CONFIG%
  echo Please extract the complete QQTang-Local release package.
  exit /b 1
)

if not exist "runtime\data" mkdir "runtime\data"
if not exist "runtime\logs" mkdir "runtime\logs"
if not exist "runtime\captures" mkdir "runtime\captures"

echo QQTang headless server is starting.
echo Press Ctrl+C in this window to stop it.
echo Network settings: configs\network.json
echo.
"%SERVER%" -config "%CONFIG%" %*
set "RESULT=%ERRORLEVEL%"

if not "%RESULT%"=="0" (
  echo.
  echo Server exited with code %RESULT%.
  echo See runtime\logs\server-local.jsonl and the error above.
)
exit /b %RESULT%
