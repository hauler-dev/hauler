# Security Documentation

## Security Features

### 1. Input Validation
- File upload size limits (100MB manifests, configurable)
- File type validation (YAML, TAR, certificates)
- Path sanitization prevents directory traversal
- Filename validation

### 2. Command Execution Safety
- No direct shell execution
- Hauler CLI called via exec.Command (safe)
- Arguments properly escaped
- `redactArgs()` masks `--password`/`-p` values before writing to log buffer
- Registry list masks stored passwords as `***`
- Environment variables controlled

### 3. API Security
- Optional API key authentication via `HAULER_UI_API_KEY` env var
- Bearer token validation on all `/api/*` routes (except `/api/health`)
- Query param fallback (`?api_key=`) for WebSocket connections
- Content-Type `application/json` set on all JSON responses
- Error messages don't leak sensitive info

### 4. File System Security
- `safePath()` calls `filepath.Base()` on all user-supplied filenames before `filepath.Join`
- Rejects `..`, `.`, and empty filenames
- Restricted write paths (/data only)
- Proper file permissions (0755 dirs, 0644 files)
- No symbolic link following
- Isolated volumes

### 5. Certificate Handling
- CA certificates validated before installation
- Stored in isolated directory
- Proper permissions enforced

## Security Best Practices

### Container Security
```
Docker Hardened Images (DHI) — non-root runtime, no shell in production image.
Init container (init-permissions) fixes bind-mount permissions before main container starts.

# Read-only root filesystem (optional enhancement)
docker run --read-only \
  --tmpfs /tmp \
  -v ./data:/data \
  hauler-ui
```

### Network Security
```yaml
# Restrict network access
networks:
  hauler-net:
    driver: bridge
    internal: true  # No external access
```

### Secrets Management
```bash
# Use Docker secrets for credentials
echo "mypassword" | docker secret create registry_password -

# Reference in compose
secrets:
  - registry_password
```

## Vulnerability Scanning

### Container Scanning
```bash
# Scan with Trivy
trivy image hauler-ui

# Scan with Grype
grype hauler-ui
```

### Dependency Scanning
```bash
# Go dependencies
cd backend
go list -json -m all | nancy sleuth

# Check for updates
go list -u -m all
```

## Security Checklist

- [x] No hardcoded credentials
- [x] Input validation on all endpoints (json.Decode error checks)
- [x] Safe command execution (exec.Command, no shell)
- [x] Path traversal prevention (safePath with filepath.Base)
- [x] XSS prevention (escapeHTML + escapeAttr in frontend)
- [x] File upload limits
- [x] Proper error handling (io.Copy checks)
- [x] Secure file permissions
- [x] Content-Type headers on all JSON responses
- [x] API key authentication (HAULER_UI_API_KEY)
- [x] Credential redaction in logs (redactArgs)
- [x] WebSocket origin validation (CheckOrigin)
- [x] Docker Hardened Images (non-root, no shell)
- [x] Certificate validation (PEM + x509 parsing)
- [ ] Rate limiting (enhancement)
- [ ] Audit logging (enhancement)
- [ ] TLS/HTTPS (enhancement)

## Threat Model

### Threats Mitigated
1. **Command Injection**: Using exec.Command, not shell
2. **Path Traversal**: filepath.Join with validation
3. **File Upload Abuse**: Size limits and type validation
4. **XSS**: No user content rendered without escaping
5. **CSRF**: Same-origin policy

### Potential Enhancements
1. **Authorization**: Role-based access control
2. **Rate Limiting**: Prevent abuse
3. **Audit Logging**: Track all operations
4. **TLS**: Encrypt traffic

## Incident Response

### Suspicious Activity
```bash
# Check logs
docker logs hauler-ui | grep -i error

# Check file access
docker exec hauler-ui ls -la /data

# Check processes
docker exec hauler-ui ps aux
```

### Recovery
```bash
# Stop container
docker-compose down

# Backup data
tar -czf incident-backup.tar.gz data/

# Restore from clean backup
rm -rf data/
tar -xzf clean-backup.tar.gz

# Restart
docker-compose up -d
```

## Compliance

### Data Privacy
- No PII collected
- No external network calls (except Hauler operations)
- All data stored locally
- No telemetry

### Audit Trail
```bash
# Enable audit logging (enhancement)
docker-compose logs > audit-$(date +%Y%m%d).log
```

## Security Updates

### Update Hauler
```bash
# Rebuild with latest Hauler
docker-compose build --no-cache
docker-compose up -d
```

### Update Dependencies
```bash
cd backend
go get -u ./...
go mod tidy
```

### Update Base Image
```dockerfile
# Uses Docker Hardened Images (DHI)
FROM dhi.io/golang:1-alpine3.21-dev AS builder
FROM dhi.io/golang:1-alpine3.21
```

## Reporting Security Issues

Report security vulnerabilities to the project maintainers.
Do not open public issues for security concerns.
