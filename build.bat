@echo off
echo === WinDivert Go Gateway - Fixed Version ===
echo.

:: Check if Go is installed
go version >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo Error: Go is not installed or not in PATH
    echo Please install Go from: https://golang.org/dl/
    pause
    exit /b 1
)

echo Go version:
go version

:: Create build directory
if not exist build mkdir build

echo.
echo Downloading dependencies...
go mod download
go mod tidy

echo.
echo Building with new GoDivert library...
echo.

:: Check if WinDivert DLLs are available
if exist WinDivert.dll (
    echo ✓ WinDivert.dll found
) else (
    echo ⚠ WinDivert.dll not found. Please download from:
    echo   https://reqrypt.org/windivert.html
    echo   Extract WinDivert.dll to the project root directory
)

if exist WinDivert64.sys (
    echo ✓ WinDivert64.sys found
) else if exist WinDivert32.sys (
    echo ✓ WinDivert32.sys found
) else (
    echo ⚠ WinDivert driver files not found. Please download from:
    echo   https://reqrypt.org/windivert.html
    echo   Extract WinDivert64.sys/WinDivert32.sys to the project root
)

echo.
echo Building for Windows...
go build -o build\windivert-gateway.exe .

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✅ Build successful!
    echo Generated: build\windivert-gateway.exe
    echo.
    echo To run the gateway:
    echo   1. Copy WinDivert.dll to build\ directory
    echo   2. Copy WinDivert64.sys to build\ directory ^(for 64-bit^)
    echo   3. Run as Administrator: build\windivert-gateway.exe
    echo.
    echo Required files in build\ directory:
    echo   - windivert-gateway.exe ^(executable^)
    echo   - WinDivert.dll
    echo   - WinDivert64.sys
    echo   - config.json
) else (
    echo.
    echo ❌ Build failed!
    echo.
    echo Common issues and solutions:
    echo 1. Download WinDivert library files:
    echo    https://reqrypt.org/windivert.html
    echo.
    echo 2. Check Go version ^(requires 1.21+^):
    echo    go version
    echo.
    echo 3. Clear Go module cache if needed:
    echo    go clean -modcache
    echo.
    pause
    exit /b 1
)

echo.
echo === Build completed ===
pause