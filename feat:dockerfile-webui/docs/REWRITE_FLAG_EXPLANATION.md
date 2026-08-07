# Rewrite Flag Explanation

## What is the `--rewrite` flag?

The `--rewrite` flag in Hauler allows you to change the registry path when adding content to the store. By default, Hauler adds a `/hauler/` prefix to content paths. The rewrite flag lets you customize this behavior.

## Where can you use `--rewrite`?

According to Hauler documentation, the `--rewrite` flag is ONLY available for:

1. **`hauler store add chart`** - When adding Helm charts
2. **`hauler store add image`** - When adding Docker images
3. **`hauler store sync`** - When syncing from manifests

## Where `--rewrite` is NOT available:

- **`hauler store save`** - Saving store to haul file (no rewrite support)
- **`hauler store copy`** - Copying/pushing to registry (no rewrite support)

## Why doesn't `--rewrite` work on the Registry Push tab?

The Registry Push tab uses `hauler store copy` command, which does NOT support the `--rewrite` flag. This is by design in Hauler itself.

**The rewrite path must be specified when ADDING content, not when pushing it.**

## How to use rewrite correctly:

### Example 1: Add chart with custom registry path
```bash
hauler store add chart rancher --repo https://releases.rancher.com/server-charts/stable --rewrite harbor.company.com/charts
```

This will store the chart with path: `harbor.company.com/charts/rancher` instead of the default `/hauler/rancher`

### Example 2: Add image with custom registry path
```bash
hauler store add image nginx:latest --rewrite harbor.company.com/library
```

This will store the image with path: `harbor.company.com/library/nginx:latest`

### Example 3: Sync with rewrite
```bash
hauler store sync --filename manifest.yaml --rewrite harbor.company.com
```

This applies the rewrite path to all content in the manifest.

## In Hauler UI:

### ✅ Rewrite is available in:
- **Add Charts** tab (direct form + batch browser modal)
- **Add Images** tab (Cosign Verification section)
- **Store** tab → Sync Store (Advanced Options)

### ❌ Rewrite is NOT available in:
- **Store** tab → Save Store (not supported by Hauler)
- **Push to Registry** tab (not supported by Hauler)

## Best Practice:

**Plan your registry paths BEFORE adding content to the store.** Once content is added with a specific path, you cannot change it during push. You would need to:

1. Clear the store
2. Re-add content with the correct `--rewrite` path
3. Then push to registry

## Why does Hauler add `/hauler/` by default?

Hauler adds the `/hauler/` prefix to avoid conflicts with existing content in registries and to clearly identify content managed by Hauler. The `--rewrite` flag gives you control over this behavior when needed.
