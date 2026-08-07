# Troubleshooting Guide

Common issues and their solutions.

## Container Issues

### Container Won't Start

**Symptoms:**
- `docker compose up` fails
- Container exits immediately

**Solutions:**

1. **Check logs:**
```bash
docker compose logs -f
```

2. **Verify port availability:**
```bash
# Check if port 8080 is in use
lsof -i :8080
# Or on Linux
netstat -tulpn | grep 8080
```

3. **Check Docker resources:**
```bash
docker system df
docker system prune  # Clean up if needed
```

4. **Rebuild container:**
```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```

### Permission Denied Errors

**Symptoms:**
- Cannot write to `/data` directories
- File upload fails

**Solutions:**

1. **Fix directory permissions:**
```bash
chmod -R 755 data/
```

2. **Check Docker volume mounts:**
```bash
docker compose down
docker volume ls
docker volume rm hauler-ui_data  # If needed
docker compose up -d
```

## Application Issues

### WebSocket Connection Failed

**Symptoms:**
- Live logs not updating
- "WebSocket connection failed" in console

**Solutions:**

1. **Check browser console:**
   - Open Developer Tools (F12)
   - Look for WebSocket errors

2. **Verify backend is running:**
```bash
curl http://localhost:8080/api/health
```

3. **Check firewall:**
   - Ensure port 8080 is open
   - Check corporate proxy settings

### Charts Not Loading

**Symptoms:**
- Repository browse shows no charts
- "Failed to fetch repository index" error

**Solutions:**

1. **Verify repository URL:**
   - Test URL in browser: `https://charts.bitnami.com/bitnami/index.yaml`

2. **Check network connectivity:**
```bash
docker exec hauler-ui curl -I https://charts.bitnami.com/bitnami/index.yaml
```

3. **Check for proxy requirements:**
   - Add proxy environment variables to `docker-compose.yml`:
```yaml
environment:
  - HTTP_PROXY=http://proxy:8080
  - HTTPS_PROXY=http://proxy:8080
```

### Store Operations Fail

**Symptoms:**
- "Hauler command failed" errors
- Store info shows empty

**Solutions:**

1. **Verify Hauler installation:**
```bash
docker exec hauler-ui hauler version
```

2. **Check store directory:**
```bash
docker exec hauler-ui ls -la /data/store
```

3. **Reset store:**
```bash
# Via UI: Settings tab → Reset System
# Or manually:
docker exec hauler-ui rm -rf /data/store/*
docker compose restart
```

## Registry Issues

### Push to Registry Fails

**Symptoms:**
- "Failed to push" errors
- Authentication errors

**Solutions:**

1. **Verify registry credentials:**
   - Test login manually:
```bash
docker exec hauler-ui hauler login registry.example.com -u user -p pass
```

2. **Check registry URL:**
   - Ensure no `http://` or `https://` prefix
   - Use just `registry.example.com`

3. **Test registry connectivity:**
```bash
docker exec hauler-ui curl -I https://registry.example.com/v2/
```

4. **Check TLS settings:**
   - For self-signed certificates, enable "Insecure" option
   - Or upload CA certificate

### Registry Login Fails

**Symptoms:**
- "Unauthorized" errors
- "Invalid credentials"

**Solutions:**

1. **Verify credentials:**
   - Check username/password
   - Ensure no extra spaces

2. **Check registry authentication method:**
   - Some registries require tokens instead of passwords
   - Generate token from registry UI

3. **Test with Docker:**
```bash
docker login registry.example.com
```

## Performance Issues

### Slow Chart Addition

**Symptoms:**
- Chart addition takes very long
- UI becomes unresponsive

**Solutions:**

1. **Check network speed:**
   - Large charts take time to download
   - Monitor logs for progress

2. **Disable image extraction:**
   - Uncheck "Add Images" option
   - Add images separately if needed

3. **Increase Docker resources:**
   - Allocate more CPU/RAM in Docker settings

### High Memory Usage

**Symptoms:**
- Container uses excessive memory
- System becomes slow

**Solutions:**

1. **Check store size:**
```bash
docker exec hauler-ui du -sh /data/store
```

2. **Clear old hauls:**
```bash
rm data/hauls/*.tar.zst
```

3. **Limit Docker memory:**
```yaml
# In docker-compose.yml
services:
  hauler-ui:
    mem_limit: 2g
```

## Data Issues

### Lost Configuration

**Symptoms:**
- Repositories disappeared
- Registry settings gone

**Solutions:**

1. **Check config files:**
```bash
ls -la data/config/
```

2. **Restore from backup:**
```bash
cp backup/repositories.json data/config/
docker compose restart
```

3. **Recreate configuration:**
   - Re-add repositories via UI
   - Reconfigure registries

### Corrupted Store

**Symptoms:**
- Store info shows errors
- Cannot add content

**Solutions:**

1. **Validate store:**
```bash
docker exec hauler-ui hauler store info
```

2. **Reset store:**
```bash
docker exec hauler-ui rm -rf /data/store/*
docker compose restart
```

3. **Restore from haul:**
   - Upload previous haul
   - Load via UI

## Browser Issues

### UI Not Loading

**Symptoms:**
- Blank page
- "Cannot connect" error

**Solutions:**

1. **Check backend status:**
```bash
docker compose ps
```

2. **Clear browser cache:**
   - Hard refresh: Ctrl+Shift+R (Cmd+Shift+R on Mac)
   - Clear site data in Developer Tools

3. **Try different browser:**
   - Test in Chrome/Firefox/Edge
   - Disable browser extensions

### JavaScript Errors

**Symptoms:**
- Console shows errors
- Features not working

**Solutions:**

1. **Check browser console:**
   - Open Developer Tools (F12)
   - Look for error messages

2. **Verify JavaScript is enabled:**
   - Check browser settings

3. **Update browser:**
   - Use latest version

## Getting More Help

### Collect Diagnostic Information

```bash
# Container logs
docker compose logs > logs.txt

# System info
docker version > system-info.txt
docker compose version >> system-info.txt

# Store info
docker exec hauler-ui hauler store info > store-info.txt
```

### Report an Issue

Include:
1. Hauler UI version
2. Docker version
3. Operating system
4. Steps to reproduce
5. Error messages
6. Logs (sanitize sensitive data!)

### Community Support

- **GitLab Issues**: Report bugs
- **Discussions**: Ask questions
- **Wiki**: Check documentation
- **Hauler Docs**: https://hauler.dev

## Quick Fixes

### Complete Reset

```bash
# Stop and remove everything
docker compose down -v

# Clean data
rm -rf data/store/* data/hauls/* data/manifests/* data/config/*

# Restart
docker compose up -d
```

### Update to Latest Version

```bash
docker compose pull
docker compose up -d
```

### Check Health

```bash
curl http://localhost:8080/api/health
```
