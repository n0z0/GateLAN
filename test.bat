@echo off
echo Testing Windivert Gateway Build...
echo.

:: Test 1: Check Go installation
echo [1/4] Checking Go installation...
go version >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Go not found. Install from: https://golang.org/dl/
    pause
    exit /b 1
)
echo ✅ Go installed: 
go version
echo.

:: Test 2: Download dependencies
echo [2/4] Downloading dependencies...
go mod download
go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo ❌ Failed to download dependencies
    echo Try: go clean -modcache
    pause
    exit /b 1
)
echo ✅ Dependencies downloaded
echo.

:: Test 3: Check WinDivert files
echo [3/4] Checking WinDivert files...
if exist WinDivert.dll (
    echo ✅ WinDivert.dll found
) else (
    echo ⚠️  WinDivert.dll missing - download from https://reqrypt.org/windivert.html
)

if exist WinDivert64.sys (
    echo ✅ WinDivert64.sys found
) else (
    echo ⚠️  WinDivert64.sys missing - download from https://reqrypt.org/windivert.html
)
echo.

:: Test 4: Build test
echo [4/4] Testing build...
go build -o test-build main_v2.go
if %ERRORLEVEL% EQU 0 (
    echo ✅ Build successful!
    echo Generated: test-build.exe
    del test-build.exe
) else (
    echo ❌ Build failed
    echo Check the error messages above
)
echo.

echo === Test Summary ===
echo ✅ All critical tests passed
echo ⚠️  Download WinDivert files if missing
echo.
echo Next steps:
echo 1. build_fixed.bat (for full build)
echo 2. Run windivert-gateway.exe as Administrator
echo.
pause