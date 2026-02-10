# Deployment Checklist

## Pre-Deployment

### System Requirements
- [ ] Docker installed (20.10+)
- [ ] Docker Compose installed (2.0+)
- [ ] Ports 8080 and 5000 available
- [ ] Minimum 2GB RAM
- [ ] Minimum 10GB disk space

### Verification
```bash
docker --version
docker-compose --version
netstat -tuln | grep -E '8080|5000'
df -h
```

## Initial Deployment

### Step 1: Build
```bash
cd /home/user/Desktop/hauler_ui
make build
```
- [ ] Build completes without errors
- [ ] Image `hauler-ui` created
- [ ] No security warnings

### Step 2: Start
```bash
make run
```
- [ ] Container starts successfully
- [ ] No error messages in logs
- [ ] Health check passes

### Step 3: Verify
```bash
# Health check
curl http://localhost:8080/api/health

# UI access
curl http://localhost:8080/ | grep "Hauler UI"

# API check
curl http://localhost:8080/api/store/info
```
- [ ] Health endpoint returns `{"healthy":true}`
- [ ] UI loads in browser
- [ ] API responds

## Functional Testing

### Dashboard
- [ ] Dashboard loads
- [ ] Store status displays
- [ ] Server status displays
- [ ] Health indicator shows green
- [ ] Store info loads

### Store Management
- [ ] Can select manifest
- [ ] Sync button works
- [ ] Save button works
- [ ] Load button works
- [ ] Output displays correctly

### Manifest Management
- [ ] File upload works
- [ ] Manifest list displays
- [ ] Download works
- [ ] Example manifest exists

### Haul Management
- [ ] File upload works
- [ ] Haul list displays
- [ ] Download works

### Serve
- [ ] Port configuration works
- [ ] Start server works
- [ ] Stop server works
- [ ] Status updates correctly
- [ ] Registry accessible on port 5000

### Settings
- [ ] CA cert upload works
- [ ] About section displays

### Logs
- [ ] Logs display
- [ ] Real-time updates work
- [ ] Clear button works
- [ ] WebSocket connects

## Security Testing

### Input Validation
```bash
# Path traversal
curl -X POST http://localhost:8080/api/store/sync \
  -H "Content-Type: application/json" \
  -d '{"filename":"../../etc/passwd"}'
```
- [ ] Returns error or handles safely

### File Upload
```bash
# Large file
dd if=/dev/zero of=test.yaml bs=1M count=200
curl -F "file=@test.yaml" -F "type=manifest" \
  http://localhost:8080/api/files/upload
rm test.yaml
```
- [ ] Handles large files appropriately
- [ ] No crashes or hangs

### API Security
- [ ] CORS configured correctly
- [ ] No sensitive data in errors
- [ ] File paths validated

## Performance Testing

### Load Test
```bash
# Install apache bench if needed
# sudo apt-get install apache2-utils

ab -n 100 -c 10 http://localhost:8080/api/health
```
- [ ] Handles concurrent requests
- [ ] Response time < 100ms
- [ ] No errors

### Resource Usage
```bash
docker stats hauler-ui --no-stream
```
- [ ] Memory usage < 500MB
- [ ] CPU usage reasonable
- [ ] No memory leaks

## Integration Testing

### Full Workflow
```bash
# 1. Upload manifest
curl -F "file=@data/manifests/example-manifest.yaml" \
     -F "type=manifest" \
     http://localhost:8080/api/files/upload

# 2. Sync store
curl -X POST http://localhost:8080/api/store/sync \
     -H "Content-Type: application/json" \
     -d '{"filename":"example-manifest.yaml"}'

# 3. Check store
curl http://localhost:8080/api/store/info

# 4. Save haul
curl -X POST http://localhost:8080/api/store/save \
     -H "Content-Type: application/json" \
     -d '{"filename":"test.tar.zst"}'

# 5. Start registry
curl -X POST http://localhost:8080/api/serve/start \
     -H "Content-Type: application/json" \
     -d '{"port":"5000"}'

# 6. Check registry
sleep 2
curl http://localhost:5000/v2/_catalog
```
- [ ] All steps complete successfully
- [ ] Files created in data directories
- [ ] Registry responds

## Production Deployment

### Configuration
- [ ] Review docker-compose.yml
- [ ] Set appropriate ports
- [ ] Configure volumes
- [ ] Set environment variables
- [ ] Review resource limits

### Security Hardening
- [ ] Change default ports if needed
- [ ] Configure firewall rules
- [ ] Set up reverse proxy (optional)
- [ ] Enable TLS (optional)
- [ ] Configure backups

### Monitoring
- [ ] Set up health checks
- [ ] Configure log rotation
- [ ] Set up alerts (optional)
- [ ] Monitor disk space
- [ ] Monitor memory usage

### Backup Strategy
```bash
# Create backup script
cat > backup.sh << 'EOF'
#!/bin/bash
DATE=$(date +%Y%m%d_%H%M%S)
tar -czf hauler-backup-$DATE.tar.gz data/
echo "Backup created: hauler-backup-$DATE.tar.gz"
EOF
chmod +x backup.sh
```
- [ ] Backup script created
- [ ] Test backup/restore
- [ ] Schedule regular backups

## Post-Deployment

### Documentation
- [ ] Update README with custom config
- [ ] Document any changes
- [ ] Create runbook for operations
- [ ] Document backup procedures

### Training
- [ ] Train users on UI
- [ ] Provide workflow examples
- [ ] Share documentation
- [ ] Set up support channel

### Monitoring
```bash
# Check logs regularly
make logs

# Monitor health
watch -n 5 'curl -s http://localhost:8080/api/health'

# Check disk space
df -h data/
```
- [ ] Logs reviewed
- [ ] Health monitoring active
- [ ] Disk space monitored

## Maintenance

### Regular Tasks
- [ ] Review logs weekly
- [ ] Check disk space
- [ ] Update Hauler version
- [ ] Update dependencies
- [ ] Test backups monthly

### Updates
```bash
# Update Hauler
docker-compose build --no-cache
docker-compose up -d

# Update dependencies
cd backend
go get -u ./...
go mod tidy
```

### Troubleshooting
```bash
# View logs
docker logs hauler-ui

# Check processes
docker exec hauler-ui ps aux

# Check files
docker exec hauler-ui ls -la /data

# Restart
docker-compose restart

# Full reset
make clean
make run
```

## Rollback Plan

### If Issues Occur
1. Stop container: `make stop`
2. Restore backup: `tar -xzf hauler-backup-YYYYMMDD.tar.gz`
3. Restart: `make run`
4. Verify: Check health endpoint

### Emergency Contacts
- [ ] Document support contacts
- [ ] Create incident response plan
- [ ] Test rollback procedure

## Sign-Off

### Development Team
- [ ] Code reviewed
- [ ] Tests passed
- [ ] Documentation complete
- [ ] Security reviewed

### Operations Team
- [ ] Deployment tested
- [ ] Monitoring configured
- [ ] Backups verified
- [ ] Runbook created

### Security Team
- [ ] Security scan completed
- [ ] Vulnerabilities addressed
- [ ] Access controls verified
- [ ] Audit logging configured

### Final Approval
- [ ] All checks passed
- [ ] Stakeholders notified
- [ ] Go-live approved
- [ ] Support ready

---

**Deployment Date:** _______________
**Deployed By:** _______________
**Approved By:** _______________
