# Senior Developer - Phase 1 GAP Fix Implementation
**Date:** 2026-01-21  
**Version:** 3.1.0  
**Status:** 🚀 IMPLEMENTATION IN PROGRESS  
**Agent:** Senior Developer

---

## Phase 1 Implementation Plan

### Target: Achieve ~70% flag coverage

**Features to Implement:**
1. ✅ Remote URL support for file addition + `--name` flag
2. ✅ Platform selection for images and charts
3. ✅ Signature verification (`--key` flag)
4. ✅ Products support for sync (`--products` flag)

---

## Implementation Details

### 1. File Addition Enhancements

**Backend Changes:**
- Modify `storeAddFileHandler()` to accept URL or file upload
- Add `--name` flag support

**Frontend Changes:**
- Add radio button: "Upload File" vs "Remote URL"
- Add URL input field
- Add custom name field

**API Changes:**
- `/api/store/add-file` - Accept JSON with `{url, name}` OR multipart file upload

---

### 2. Platform Selection

**Backend Changes:**
- Update `AddContentRequest` struct with `Platform` field
- Pass `--platform` flag to hauler commands

**Frontend Changes:**
- Add platform dropdown to image form
- Add platform dropdown to chart form
- Options: `linux/amd64`, `linux/arm64`, `linux/arm/v7`, `all`

---

### 3. Signature Verification

**Backend Changes:**
- Add key file upload endpoint
- Store keys in `/data/config/keys/`
- Add `--key` flag support for images and sync

**Frontend Changes:**
- Add key file upload to image form
- Add key file upload to sync form
- Display uploaded keys

---

### 4. Products Support

**Backend Changes:**
- Update sync handler to accept `products` parameter
- Pass `--products` flag to hauler sync

**Frontend Changes:**
- Add products input field to sync form
- Add helper text with format example
- Add quick-select buttons for common products

---

## Code Changes

### Files to Modify:
1. `backend/main.go` - 4 handler updates
2. `frontend/app.js` - 4 function updates
3. `frontend/index.html` - 4 UI section updates

---

**Estimated Effort:** 2-3 hours  
**Testing Required:** All 4 features  
**Documentation:** Update README with new features
