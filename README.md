# WinDivert Gateway - Fixed Version (2025)

⚠️ **PERHATIAN: SOLUSI UNTUK MASALAH DEPENDENCY**

Gateway Windows yang menggunakan WinDivert untuk mengarahkan traffic HTTP dan HTTPS ke proxy menggunakan Go dengan library GoDivert yang aktif.

## 🚨 Masalah & Solusi

### Masalah yang Dihadapi:
```
go: windivert-gateway imports
        github.com/basil00/go-windivert: github.com/basil00/go-windivert@v1.3.0: 
        reading https://proxy.golang.org/github.com/basil00/go-windivert/@v/v1.3.0.zip: 
        404 Not Found
```

### Solusi:
Repository asli `basil00/go-windivert` tidak tersedia lagi. Kami menggunakan alternatif aktif:
- **Library Baru:** `github.com/TryPerzh/GoDivert v1.0.0`
- **Status:** Aktif (Terakhir update: Jun 2025)
- **Compatible:** WinDivert 2.2

## 📦 Dependencies Baru

```go
require (
    github.com/TryPerzh/GoDivert v1.0.0
)
```

## 🛠️ Installation Steps

### 1. Install GoDivert Library

```bash
go mod tidy
```

### 2. Download WinDivert Files

Download dari official website: **https://reqrypt.org/windivert.html**

**File yang dibutuhkan:**
- `WinDivert.dll` - Main library
- `WinDivert64.sys` - Driver untuk 64-bit Windows
- `WinDivert32.sys` - Driver untuk 32-bit Windows (opsional)

### 3. Build Gateway

```bash
# Windows
build_fixed.bat

# Linux/Mac
chmod +x build_fixed.sh
./build_fixed.sh
```

### 4. Setup Run Directory

Copy file-file ini ke direktori build:
```
build/
├── windivert-gateway.exe  (executable)
├── WinDivert.dll          (dari download)
├── WinDivert64.sys        (dari download)
└── config.json           (dari project)
```

### 5. Jalankan sebagai Administrator

```bash
cd build
# Run as Administrator
./windivert-gateway.exe
```

## 📁 File Structure (Updated)

```
windivert-gateway/
├── main_v2.go              # Main application (NEW - GoDivert compatible)
├── config.go               # Configuration handling
├── packet_handler.go       # Packet processing (legacy - may need updates)
├── proxy_server.go         # Proxy server implementation
├── go.mod                  # Updated dependencies
├── config.json             # Configuration file
├── build_fixed.bat         # Windows build script (NEW)
├── build_fixed.sh          # Linux/Mac build script (NEW)
├── README_FIXED.md         # This file
└── WinDivert files/        # Download from https://reqrypt.org/windivert.html
    ├── WinDivert.dll
    ├── WinDivert64.sys
    └── WinDivert32.sys
```

## 🔧 Configuration

Edit `config.json`:

```json
{
  "proxy_addr": "127.0.0.1:8080",
  "local_addr": "127.0.0.1", 
  "http_port": 80,
  "https_port": 443,
  "buffer_size": 65535,
  "packet_queue": 1000
}
```

## ⚙️ Build Tags (Optional)

Jika ingin menggunakan fitur CGO:

```bash
# Build dengan CGO support
go build -tags="divert_cgo" -o windivert-gateway.exe .

# Build dengan resource loading
go build -tags="divert_rsrc" -o windivert-gateway.exe .
```

## 🔍 Troubleshooting

### 1. Build Errors

**Masalah:** `WinDivert.dll not found`
```bash
# Solusi: Download WinDivert files
# https://reqrypt.org/windivert.html
```

**Masalah:** `go mod tidy failed`
```bash
# Solusi: Clear cache
go clean -modcache
go mod download
```

### 2. Runtime Errors

**Masalah:** `Access Denied`
```bash
# Solusi: Run as Administrator
```

**Masalah:** `Driver not loaded`
```bash
# Solusi: Install WinDivert driver
# Extract WinDivert64.sys to project directory
```

### 3. Connection Issues

**Masalah:** `Connection refused to proxy`
```bash
# Pastikan proxy server berjalan
# Verify proxy_addr di config.json
```

## 📊 API Changes (GoDivert vs Basil00)

| Basil00/go-windivert | GoDivert |
|---------------------|----------|
| `windivert.Open()` | `GoDivert.Open()` |
| `packet.Read()` | `wd.Recv()` |
| `packet.Reinject()` | `wd.Send()` |
| `packet.TCP()` | `packet.TCP` |
| Built-in parsing | `helper.ParsePacket()` |

## 🔗 Resources

- **WinDivert Official:** https://reqrypt.org/windivert.html
- **GoDivert Library:** https://github.com/TryPerzh/GoDivert
- **Go Documentation:** https://golang.org/doc/
- **Windivert Documentation:** https://reqrypt.org/windivert-doc.html

## 🎯 Quick Test

1. **Setup:** Download WinDivert files
2. **Build:** `build_fixed.bat`
3. **Run:** Administrator mode
4. **Test:** Browser → check proxy logs

## ⚡ Performance Notes

- **Buffer Size:** Larger = better throughput (65535 default)
- **Packet Queue:** More = less packet loss (1000 default)
- **Driver:** WinDivert 2.2 required

---

**Status:** ✅ Fixed - Library dependency resolved
**Last Updated:** 2025-11-16 21:42:32