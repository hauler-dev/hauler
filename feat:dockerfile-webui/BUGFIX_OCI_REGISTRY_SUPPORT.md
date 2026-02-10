# Bug Fix: OCI Registry Support in Repository Browser

## Issue
The "Repositories" menu's "Browse" feature did not work with OCI registries (URLs starting with `oci://`). When users tried to browse charts from an OCI registry, the application would fail because it attempted to fetch an `index.yaml` file that doesn't exist in OCI registries.

## Root Cause
OCI (Open Container Initiative) registries store Helm charts differently than traditional HTTP-based Helm repositories:

- **Traditional Helm Repos**: Have an `index.yaml` file that lists all available charts and versions
- **OCI Registries**: Store charts as OCI artifacts without a browsable index

The `repoChartsHandler` function in the backend always tried to fetch `index.yaml`, which doesn't exist for OCI registries.

## Fix Applied

### Backend Changes (`backend/main.go`)

Updated the `repoChartsHandler` function to detect OCI registries and return a helpful message:

```go
func repoChartsHandler(w http.ResponseWriter, r *http.Request) {
    // ... repository lookup code ...
    
    // Check if this is an OCI registry
    if strings.HasPrefix(repo.URL, "oci://") {
        // OCI registries don't have browsable indexes
        json.NewEncoder(w).Encode(map[string]interface{}{
            "charts":  map[string][]string{},
            "details": map[string]ChartInfo{},
            "isOCI":   true,
            "message": "OCI registries cannot be browsed. Please use 'Add Chart Directly' tab and specify the chart name manually.",
        })
        return
    }
    
    // ... existing index.yaml fetching logic for traditional repos ...
}
```

### Frontend Changes (`frontend/app.js`)

Updated the `browseRepoCharts` function to handle OCI registries gracefully:

```javascript
async function browseRepoCharts(repoName) {
    const data = await apiCall(`repos/charts/${repoName}`);
    
    // Check if this is an OCI registry
    if (data.isOCI) {
        alert(`OCI Registry Detected\n\n${data.message}\n\nOCI registries (oci://) don't support browsing. You'll need to:\n1. Go to the "Add Charts" tab\n2. Manually enter the chart name\n3. Specify the OCI repository URL`);
        return;
    }
    
    // ... existing chart browser logic ...
}
```

## User Experience

### Before Fix
- User adds OCI registry (e.g., `oci://registry.example.com/charts`)
- User clicks "Browse" button
- Application shows error: "Failed to fetch repository index" or "Repository index not found"
- User is confused and cannot proceed

### After Fix
- User adds OCI registry (e.g., `oci://registry.example.com/charts`)
- User clicks "Browse" button
- Application shows clear message explaining:
  - OCI registries cannot be browsed
  - User should use "Add Chart Directly" tab instead
  - Instructions on how to add charts manually
- User understands the limitation and knows how to proceed

## Workaround for OCI Registries

Users can still add charts from OCI registries using the "Add Charts" tab:

1. Navigate to "Add Charts" tab
2. Enter the chart name (e.g., `my-chart`)
3. Enter the OCI repository URL (e.g., `oci://registry.example.com/charts`)
4. Specify version (optional)
5. Click "Add Chart to Store"

## Technical Details

### OCI Registry Format
OCI registries use the format: `oci://registry.example.com/path/to/chart`

### Why OCI Registries Can't Be Browsed
- OCI registries follow the OCI Distribution Specification
- They don't provide a catalog API for listing all artifacts
- Each chart must be accessed by its exact name
- This is by design for security and scalability

### Traditional Helm Repository Format
Traditional repos use HTTP/HTTPS with an `index.yaml` file:
- Format: `https://charts.example.com/`
- Index file: `https://charts.example.com/index.yaml`
- Contains metadata for all charts and versions

## Files Modified
- `backend/main.go` - Added OCI detection in `repoChartsHandler`
- `frontend/app.js` - Added OCI handling in `browseRepoCharts`

## Testing
1. Add an OCI registry: `oci://ghcr.io/helm/charts`
2. Click "Browse" button
3. Verify helpful message is displayed
4. Go to "Add Charts" tab
5. Manually add a chart from the OCI registry
6. Verify chart is added successfully

## Version
- Fixed in: v3.3.5 (patched)
- Date: 2026-01-30
