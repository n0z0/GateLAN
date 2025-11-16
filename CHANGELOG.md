# Quick Fix Guide - Windivert Gateway Dependency Issue

## 🚨 Problem Summary

**Error yang Anda alami:**
```bash
go mod tidy
go: downloading github.com/basil00/go-windivert v1.3.0
go: windivert-gateway imports
        github.com/basil00/go-windivert: github.com/basil00/go-windivert@v1.3.0: 
        reading https://proxy.golang.org/github.com/basil00/go-windivert/@v/v1.3.0.zip: 
        404 Not Found
        server response:
        not found: github.com/basil00/go-windivert@v1.3.0: invalid version: 
        git ls-remote -q https://github.com/basil00/go-windivert in /tmp/gopath/pkg/mod/cache/vcs/
```

## ✅ Solution - Step by Step

### Step 1: Clear Cache
```bash
go clean -modcache
go mod download
```

### Step 2: Update Dependencies
Saya sudah update `go.mod` dengan library baru:
```go
// OLD (broken):
github.com/basil00/go-windivert v1.3.0

// NEW (working):
github.com/TryPerzh/GoDivert v1.0.0
```

### Step 3: Download WinDivert Files
**MANDATORY:** Download dari https://reqrypt.org/windivert.html

**File yang dibutuhkan:**
- `WinDivert.dll`
- `WinDivert64.sys`
- `WinDivert32.sys` (jika perlu)

### Step 4: Build dengan Script Baru
```bash
# Windows:
build_fixed.bat

# Linux/Mac:
chmod +x build_fixed.sh
./build_fixed.sh
```

### Step 5: Setup Runtime Files
Copy semua file ini ke direktori `build/`:
```
build/
├── windivert-gateway.exe
├── WinDivert.dll
├── WinDivert64.sys
├── WinDivert32.sys (optional)
└── config.json
```

### Step 6: Run as Administrator
```bash
cd build
# Klik kanan → "Run as administrator"
windivert-gateway.exe
```

## 📝 Updated Files

1. **go.mod** - Dependencies baru
2. **main_v2.go** - Code kompatibel dengan GoDivert
3. **build_fixed.bat** - Build script untuk Windows
4. **build_fixed.sh** - Build script untuk Linux/Mac
5. **README_FIXED.md** - Dokumentasi lengkap

## 🔍 Verification

### Check Library Status:
```bash
go list -m -versions github.com/TryPerzh/GoDivert
# Should show: v1.0.0 v1.0.1 (etc)
```

### Check Build:
```bash
# Should compile without errors
go build -o test-build main_v2.go
```

### Check Dependencies:
```bash
go mod graph | grep -i windivert
# Should show TryPerzh/GoDivert, not basil00/go-windivert
```

## 🆘 If Still Having Issues

### Issue 1: "WinDivert.dll not found"
**Solution:** Download dari https://reqrypt.org/windivert.html

### Issue 2: "Access Denied"
**Solution:** Run as Administrator

### Issue 3: "Driver not loaded"
**Solution:** Install WinDivert driver dengan:
```bash
# Extract WinDivert64.sys ke project root
# atau copy ke C:\Windows\System32\ (administrator required)
```

### Issue 4: "go mod tidy still fails"
**Solution:** Manual replacement:
```bash
# Delete go.sum
rm go.sum

# Replace in go.mod:
# github.com/basil00/go-windivert v1.3.0
# dengan:
# github.com/TryPerzh/GoDivert v1.0.0

# Then:
go mod tidy
```

## 🎯 Quick Test Commands

```bash
# 1. Check Go version
go version

# 2. Check module
go list -m github.com/TryPerzh/GoDivert

# 3. Build test
go build -o test main_v2.go

# 4. List dependencies
go list -m all | grep -i divert
```

## 📊 Expected Output

**Success indicators:**
- ✅ `go mod tidy` completes without errors
- ✅ `build_fixed.bat` builds executable
- ✅ `windivert-gateway.exe` runs without errors
- ✅ Gateway logs show packet interception

## 🔗 Quick Links

- **WinDivert Download:** https://reqrypt.org/windivert.html
- **GoDivert GitHub:** https://github.com/TryPerzh/GoDivert
- **Go Installation:** https://golang.org/dl/

---

**TL;DR:** Repository asli `basil00/go-windivert` sudah tidak aktif. Gunakan `TryPerzh/GoDivert` + download file WinDivert dari official website.