# Hauler UI - Deployment & Testing Guide

## Quick Start

```bash
# Build and run
make build
make run

# Access UI
open http://localhost:8080
```

## Testing Checklist

### 1. Architecture Design ✓
- [x] Go backend wraps Hauler CLI
- [x] REST API + WebSocket for logs
- [x] Vanilla JS frontend (lightweight)
- [x] Docker containerized
- [x] Persistent volumes

### 2. Code Creation ✓
- [x] Backend API (Go)
- [x] Frontend UI (HTML/JS/Tailwind)
- [x] Dockerfile (multi-stage)
- [x] Docker Compose
- [x] Example manifest

### 3. Debug/Testing

#### Backend Tests
```bash
# Test Go build
cd backend
go mod download
go build -o hauler-ui main.go
./hauler-ui
```

#### API Tests
```bash
# Health check
curl http://localhost:8080/api/health

# Store info
curl http://localhost:8080/api/store/info

# Server status
curl http://localhost:8080/api/serve/status
```

#### Frontend Tests
- Navigate all tabs
- Upload manifest file
- Upload haul file
- Sync store from manifest
- Save store to haul
- Load haul to store
- Start/stop registry server
- Upload CA certificate
- View live logs

### 4. QA Testing

#### Functional Tests
- [ ] Dashboard displays correctly
- [ ] Store sync works with manifest
- [ ] Store save creates haul file
- [ ] Store load imports haul
- [ ] File upload (manifest/haul) works
- [ ] File download works
- [ ] Registry server starts/stops
- [ ] CA cert upload works
- [ ] Live logs stream correctly
- [ ] All navigation works

#### Integration Tests
```bash
# Full workflow test
cd /home/user/Desktop/hauler_ui

# 1. Start container
docker-compose up -d

# 2. Wait for startup
sleep 5

# 3. Check health
curl http://localhost:8080/api/health

# 4. Upload manifest
curl -F "file=@data/manifests/example-manifest.yaml" \
     -F "type=manifest" \
     http://localhost:8080/api/files/upload

# 5. Sync store
curl -X POST http://localhost:8080/api/store/sync \
     -H "Content-Type: application/json" \
     -d '{"filename":"example-manifest.yaml"}'

# 6. Check store
curl http://localhost:8080/api/store/info

# 7. Save haul
curl -X POST http://localhost:8080/api/store/save \
     -H "Content-Type: application/json" \
     -d '{"filename":"test.tar.zst"}'

# 8. Start registry
curl -X POST http://localhost:8080/api/serve/start \
     -H "Content-Type: application/json" \
     -d '{"port":"5000"}'

# 9. Check registry
curl http://localhost:5000/v2/_catalog
```

### 5. Security Testing

#### Input Validation
- [ ] File upload size limits enforced
- [ ] File type validation works
- [ ] Path traversal prevented
- [ ] Command injection prevented
- [ ] XSS protection in UI

#### Security Checks
```bash
# Check for shell injection
curl -X POST http://localhost:8080/api/store/sync \
     -H "Content-Type: application/json" \
     -d '{"filename":"../../etc/passwd"}'
# Should fail safely

# Check file upload limits
dd if=/dev/zero of=large.yaml bs=1M count=200
curl -F "file=@large.yaml" -F "type=manifest" \
     http://localhost:8080/api/files/upload
# Should handle gracefully

# Check CA cert validation
echo "invalid cert" > bad.crt
curl -F "cert=@bad.crt" http://localhost:8080/api/cert/upload
# Should validate
```

#### Container Security
```bash
# Check running as non-root (optional enhancement)
docker exec hauler-ui whoami

# Check exposed ports
docker port hauler-ui

# Check volume permissions
docker exec hauler-ui ls -la /data
```

## Performance Testing

```bash
# Concurrent requests
ab -n 100 -c 10 http://localhost:8080/api/health

# Large file upload
dd if=/dev/zero of=large.tar.zst bs=1M count=100
time curl -F "file=@large.tar.zst" -F "type=haul" \
     http://localhost:8080/api/files/upload
```

## Troubleshooting

### Container won't start
```bash
docker logs hauler-ui
docker-compose logs -f
```

### Hauler command fails
```bash
docker exec -it hauler-ui sh
hauler version
hauler store info
```

### Permission errors
```bash
sudo chown -R $USER:$USER data/
chmod -R 755 data/
```

### Port conflicts
```bash
# Change ports in docker-compose.yml
ports:
  - "8081:8080"  # UI
  - "5001:5000"  # Registry
```

## Production Deployment

### Environment Variables
```yaml
environment:
  - HAULER_STORE=/data/store
  - LOG_LEVEL=info
  - MAX_UPLOAD_SIZE=1073741824  # 1GB
```

### Reverse Proxy (Nginx)
```nginx
server {
    listen 80;
    server_name hauler.example.com;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### SSL/TLS
```bash
# Add to docker-compose.yml
volumes:
  - ./certs:/certs
environment:
  - TLS_CERT=/certs/cert.pem
  - TLS_KEY=/certs/key.pem
```

## Monitoring

### Health Checks
```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/api/health"]
  interval: 30s
  timeout: 10s
  retries: 3
```

### Logs
```bash
# View logs
docker-compose logs -f

# Export logs
docker logs hauler-ui > hauler-ui.log 2>&1
```

## Backup & Restore

### Backup
```bash
# Backup data
tar -czf hauler-backup-$(date +%Y%m%d).tar.gz data/

# Backup specific store
docker exec hauler-ui hauler store save \
  --filename /data/hauls/backup-$(date +%Y%m%d).tar.zst
```

### Restore
```bash
# Restore data
tar -xzf hauler-backup-20240101.tar.gz

# Restore store
docker exec hauler-ui hauler store load \
  --filename /data/hauls/backup-20240101.tar.zst
```

## Cleanup

```bash
# Stop and remove
make clean

# Remove all data
rm -rf data/store/* data/hauls/* data/config/*

# Remove images
docker rmi hauler-ui
```
