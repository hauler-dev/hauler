# Bug Fix: CA Certificate Not Used When Pushing to Registry

## Issue
When uploading a CA certificate via the Settings page, the certificate was not being used when pushing content to a private registry. This caused SSL/TLS verification failures when connecting to registries with self-signed or custom CA certificates.

## Root Cause
The uploaded CA certificate was saved to `/data/config/ca-cert.crt` and installed system-wide using `update-ca-certificates`, but the Hauler CLI commands were not configured to use this certificate. The `SSL_CERT_FILE` environment variable was not being set when executing Hauler commands.

## Fix Applied
Updated three functions in `backend/main.go` to include the CA certificate in the environment:

### 1. executeHauler Function (Primary Fix)
Added logic to check for the CA certificate and set `SSL_CERT_FILE` environment variable:

```go
func executeHauler(command string, args ...string) (string, error) {
    fullArgs := append([]string{command}, args...)
    cmd := exec.Command("hauler", fullArgs...)
    env := append(os.Environ(), "HAULER_STORE=/data/store")
    
    // Add CA certificate if it exists
    if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
        env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
    }
    cmd.Env = env
    // ... rest of function
}
```

### 2. registryPushHandler Function
Updated the registry login command within the push handler to include CA certificate:

```go
if reg.Username != "" && reg.Password != "" {
    cmd := exec.Command("hauler", "login", reg.URL, "-u", reg.Username, "-p", reg.Password)
    env := append(os.Environ(), "HAULER_STORE=/data/store")
    // Add CA certificate if it exists
    if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
        env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
    }
    cmd.Env = env
    // ... rest of function
}
```

### 3. registryLoginHandler Function
Updated the standalone registry login handler to include CA certificate:

```go
func registryLoginHandler(w http.ResponseWriter, r *http.Request) {
    // ... request parsing
    cmd := exec.Command("hauler", "login", req.Registry, "-u", req.Username, "-p", req.Password)
    env := append(os.Environ(), "HAULER_STORE=/data/store")
    // Add CA certificate if it exists
    if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
        env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
    }
    cmd.Env = env
    // ... rest of function
}
```

## Impact
This fix ensures that:
- All Hauler CLI commands respect the uploaded CA certificate
- Registry push operations work with self-signed certificates
- Registry login operations work with custom CA certificates
- Store sync operations can pull from registries with custom CAs
- Image and chart additions from private registries work correctly

## Files Modified
- `backend/main.go` (3 functions updated)

## Testing
1. Upload a CA certificate via Settings → CA Certificate
2. Configure a private registry that uses the custom CA
3. Attempt to push content to the registry
4. Verify the operation succeeds without SSL/TLS errors
5. Check logs to confirm the certificate is being used

## Environment Variable
The fix uses the `SSL_CERT_FILE` environment variable, which is the standard way to specify a custom CA certificate for SSL/TLS connections in Go and most HTTP clients.

## Version
- Fixed in: v3.3.5 (patched)
- Date: 2026-01-30
