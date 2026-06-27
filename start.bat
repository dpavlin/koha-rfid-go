@echo off
REM RFID 3M 810 Reader – Koha Staff Station
REM Double-click to start, or run from command prompt.
REM
REM Usage:
REM   start.bat                    (uses COM3, localhost:9000)
REM   start.bat COM5               (uses COM5)
REM   start.bat COM5 8080          (uses COM5, port 8080)
REM   start.bat COM5 8080 --debug  (with debug logging)

set COM=COM3
set PORT=localhost:9000
set DEBUG=

if not "%1" == "" set COM=%1
if not "%2" == "" set PORT=%2
if "%3" == "--debug" set DEBUG=-debug

echo Starting RFID server on %PORT% using %COM% ...
koha-rfid.exe -com %COM% -listen %PORT% %DEBUG%

echo.
echo Press any key to exit.
pause >nul
