# Bug Fix: Haul File Delete Not Working

## Issue
When attempting to delete a Haul file from the "Hauls" (Haul Management) tab, clicking the delete icon did not actually delete the file.

## Root Cause
The `deleteFile` function in `frontend/app.js` was not properly URL-encoding the filename when making the DELETE request to the backend API. This caused issues when filenames contained special characters or spaces.

## Fix Applied
Updated the `deleteFile` function to use `encodeURIComponent()` when constructing the API endpoint URL:

**Before:**
```javascript
const res = await fetch(`/api/files/delete/${filename}?type=${type}`, { method: 'DELETE' });
```

**After:**
```javascript
const res = await fetch(`/api/files/delete/${encodeURIComponent(filename)}?type=${type}`, { method: 'DELETE' });
```

## Files Modified
- `frontend/app.js` (line 445)

## Testing
1. Navigate to the "Hauls" tab in the Hauler UI
2. Upload or create a haul file
3. Click the delete (trash) icon next to the haul file
4. Confirm the deletion in the warning dialog
5. Verify the file is successfully deleted and removed from the list

## Backend Compatibility
The backend endpoint `/api/files/delete/{filename}` in `backend/main.go` already properly handles URL-encoded filenames using `mux.Vars(r)`, so no backend changes were required.

## Version
- Fixed in: v3.3.5 (patched)
- Date: 2026-01-30
