#!/bin/bash

echo "=== WinDivert Go Gateway - Fixed Version ==="
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed or not in PATH"
    echo "Please install Go from: https://golang.org/dl/"
    exit 1
fi

echo "Go version: $(go version)"

# Create build directory
mkdir -p build

# Download dependencies using the new library
echo ""
echo "Downloading dependencies..."
go mod download
go mod tidy

echo ""
echo "Building with new GoDivert library..."
echo ""

# Check if WinDivert DLLs are available
if [ -f "WinDivert.dll" ]; then
    echo "✓ WinDivert.dll found"
else
    echo "⚠ WinDivert.dll not found. Please download from:"
    echo "  https://reqrypt.org/windivert.html"
    echo "  Extract WinDivert.dll to the project root directory"
fi

if [ -f "WinDivert64.sys" ] || [ -f "WinDivert32.sys" ]; then
    echo "✓ WinDivert driver files found"
else
    echo "⚠ WinDivert driver files not found. Please download from:"
    echo "  https://reqrypt.org/windivert.html"
    echo "  Extract WinDivert64.sys/WinDivert32.sys to the project root"
fi

echo ""
echo "Building for current platform..."
go build -o build/windivert-gateway .

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Build successful!"
    echo "Generated: build/windivert-gateway"
    echo ""
    echo "To run the gateway:"
    echo "  1. Copy WinDivert.dll to build/ directory"
    echo "  2. Copy WinDivert64.sys to build/ directory (for 64-bit)"
    echo "  3. Run as Administrator: ./build/windivert-gateway"
    echo ""
    echo "Required files in build/ directory:"
    echo "  - windivert-gateway (executable)"
    echo "  - WinDivert.dll"
    echo "  - WinDivert64.sys"
    echo "  - config.json"
else
    echo ""
    echo "❌ Build failed!"
    echo ""
    echo "Common issues and solutions:"
    echo "1. Download WinDivert library files:"
    echo "   https://reqrypt.org/windivert.html"
    echo ""
    echo "2. Check Go version (requires 1.21+):"
    echo "   go version"
    echo ""
    echo "3. Clear Go module cache if needed:"
    echo "   go clean -modcache"
    echo ""
    exit 1
fi

echo ""
echo "=== Build completed ==="