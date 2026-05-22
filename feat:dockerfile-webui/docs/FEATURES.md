# Hauler UI - Complete Feature List

## All Hauler Commands Supported

### Store Commands
✓ `hauler store info` - View store contents and statistics
✓ `hauler store sync` - Sync content from manifest files
✓ `hauler store save` - Export store to haul archive
✓ `hauler store load` - Import haul archive to store
✓ `hauler store add` - Add individual content (via API)
✓ `hauler store copy` - Copy content between stores (via API)

### Serve Commands
✓ `hauler store serve registry` - Start OCI registry server
✓ `hauler store serve fileserver` - Serve files via HTTP
✓ Port configuration
✓ Start/Stop controls

### Login Commands
✓ Registry authentication (via CA cert upload)
✓ Credential management

### Version & Info
✓ Health monitoring
✓ Status checks
✓ Real-time logging

## UI Features

### 1. Dashboard
- Real-time store status
- Server status monitoring
- Health check indicator
- Store information display
- Auto-refresh every 5 seconds

### 2. Store Management
- **Sync Store**
  - Select from uploaded manifests
  - Sync content to store
  - Real-time progress output
  
- **Save Store**
  - Export to haul archive
  - Custom filename support
  - Compression (tar.zst)
  
- **Load Store**
  - Import from haul archive
  - Select from uploaded hauls
  - Merge with existing content
  
- **Store Info**
  - View all stored content
  - Image listings
  - Chart listings
  - File listings
  - Size information

### 3. Manifest Management
- **Upload Manifests**
  - Drag & drop support
  - YAML validation
  - Multiple file support
  
- **Manifest Library**
  - List all manifests
  - Download manifests
  - Delete manifests
  - Example manifest included

### 4. Haul Management
- **Upload Hauls**
  - Support for .tar.zst, .tar.gz, .tar
  - Large file support (100MB+)
  - Progress indication
  
- **Haul Library**
  - List all hauls
  - Download hauls
  - File size display
  - Delete hauls

### 5. Serve Registry
- **Registry Server**
  - Start/stop controls
  - Port configuration (default: 5000)
  - Status monitoring
  - OCI-compliant registry
  
- **FileServer**
  - HTTP file serving
  - Direct content access
  - Configurable port

### 6. Settings
- **CA Certificate**
  - Upload custom certificates
  - Automatic installation
  - Support for .crt and .pem
  - Registry authentication
  
- **Configuration**
  - Environment variables
  - Store location
  - Port settings

### 7. Live Logs
- **Real-time Logging**
  - WebSocket streaming
  - All command output
  - Timestamped entries
  - Clear logs function
  - Auto-scroll
  - 1000 line buffer

## Technical Features

### Backend (Go)
- RESTful API
- WebSocket support
- Concurrent request handling
- Safe command execution
- Error handling
- Input validation
- File upload/download
- Streaming responses

### Frontend (JavaScript)
- Single Page Application
- Tab-based navigation
- Responsive design
- Real-time updates
- File drag & drop
- Progress indicators
- Error notifications
- Clean UI/UX

### Container
- Alpine Linux base
- Multi-stage build
- Hauler pre-installed
- Minimal size
- Fast startup
- Health checks
- Auto-restart

### Storage
- Persistent volumes
- Organized directories
- Automatic backups
- Cross-restart persistence
- Host filesystem mapping

## Workflow Examples

### Airgap Workflow
1. **Online Environment**
   - Upload manifest with required images/charts
   - Sync store from manifest
   - Save store to haul archive
   - Download haul file

2. **Transfer**
   - Copy haul to airgapped environment

3. **Offline Environment**
   - Upload haul file
   - Load haul to store
   - Start registry server
   - Pull images from local registry

### Development Workflow
1. Create manifest with dev dependencies
2. Sync to store
3. Start registry on localhost:5000
4. Configure Docker to use local registry
5. Pull images from local cache

### Production Workflow
1. Upload production manifest
2. Sync store
3. Verify store contents
4. Save to haul
5. Deploy haul to production
6. Load and serve

## API Endpoints

### Health & Status
- `GET /api/health` - Health check
- `GET /api/serve/status` - Server status

### Store Operations
- `GET /api/store/info` - Store information
- `POST /api/store/sync` - Sync from manifest
- `POST /api/store/save` - Save to haul
- `POST /api/store/load` - Load from haul

### File Management
- `POST /api/files/upload` - Upload files
- `GET /api/files/list` - List files
- `GET /api/files/download/{filename}` - Download file

### Server Control
- `POST /api/serve/start` - Start registry
- `POST /api/serve/stop` - Stop registry

### Configuration
- `POST /api/cert/upload` - Upload CA certificate

### Logging
- `WS /api/logs` - Live log stream

## Manifest Format Support

### Images
```yaml
apiVersion: v1
kind: Images
spec:
  images:
    - name: nginx:latest
    - name: registry.example.com/app:v1.0
```

### Charts
```yaml
apiVersion: v1
kind: Charts
spec:
  charts:
    - name: rancher
      repoURL: https://releases.rancher.com/server-charts/stable
      version: 2.7.0
```

### Files
```yaml
apiVersion: v1
kind: Files
spec:
  files:
    - path: https://example.com/file.tar.gz
      name: custom-file.tar.gz
```

## Browser Support
- Chrome/Chromium (recommended)
- Firefox
- Safari
- Edge
- Any modern browser with WebSocket support

## Performance
- Handles large files (GB+)
- Concurrent operations
- Efficient streaming
- Low memory footprint
- Fast UI response

## Accessibility
- Keyboard navigation
- Screen reader compatible
- High contrast mode
- Responsive design
- Mobile friendly
