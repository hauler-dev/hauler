# Technology Stack

## Programming Languages

### Go 1.21
**Usage**: Backend server  
**File**: `backend/main.go`  
**Purpose**: HTTP API server, WebSocket server, Hauler CLI integration

**Key Libraries**:
- `github.com/gorilla/mux v1.8.1` - HTTP routing and middleware
- `github.com/gorilla/websocket v1.5.1` - WebSocket support for live logs
- `gopkg.in/yaml.v2 v2.4.0` - YAML parsing for manifests
- `golang.org/x/net v0.17.0` - Network utilities

### JavaScript (ES6+)
**Usage**: Frontend application  
**File**: `frontend/app.js`  
**Purpose**: Single-page web application, UI interactions, WebSocket client

**Features Used**:
- Fetch API for HTTP requests
- WebSocket API for real-time logs
- DOM manipulation
- Event handling
- FormData for file uploads

### Python 3.x
**Usage**: MCP server  
**File**: `mcp_server/mcp-command-server.py`  
**Purpose**: Model Context Protocol integration for AI assistants

## Frontend Technologies

### Tailwind CSS 3.x
**File**: `frontend/tailwind.min.js`  
**Purpose**: Utility-first CSS framework  
**Usage**: Responsive design, component styling, layout

### Font Awesome 6.x
**Files**: `frontend/fontawesome.min.css`, `frontend/webfonts/`  
**Purpose**: Icon library  
**Usage**: UI icons (charts, images, files, settings, etc.)

### Vanilla JavaScript
**Approach**: No framework dependencies  
**Benefits**: Minimal bundle size, airgap compatibility, no build step required

## Backend Technologies

### Gorilla Mux
**Purpose**: HTTP router and URL matcher  
**Usage**: 37 REST API endpoints with pattern matching

**Endpoint Patterns**:
```go
router.HandleFunc("/api/store/add/chart", handleAddChart).Methods("POST")
router.HandleFunc("/api/repositories/{name}/charts", handleBrowseCharts).Methods("GET")
router.HandleFunc("/ws/logs", handleWebSocket)
```

### Gorilla WebSocket
**Purpose**: WebSocket protocol implementation  
**Usage**: Real-time log streaming from Hauler CLI to browser

**Features**:
- Bidirectional communication
- Message broadcasting
- Connection management

## Build System

### Docker Multi-Stage Build
**File**: `Dockerfile`

**Stages**:
1. **Go Builder**: Compile Go backend
2. **JavaScript Obfuscator**: Obfuscate frontend code
3. **Final Image**: Alpine Linux with compiled assets

**Obfuscation**:
- Tool: `javascript-obfuscator`
- Options: Control flow flattening, dead code injection, string array encoding

### Docker Compose
**File**: `docker-compose.yml`  
**Version**: 3.8

**Configuration**:
- Single service: `hauler-ui`
- Ports: 8080 (HTTP), 5000 (serve mode)
- Volumes: 4 persistent mounts
- Restart policy: `unless-stopped`

### Makefile
**File**: `Makefile`  
**Purpose**: Build automation and common tasks

## Dependencies

### Go Dependencies
```
module hauler-ui

go 1.18

require (
    github.com/gorilla/mux v1.8.1
    github.com/gorilla/websocket v1.5.1
    gopkg.in/yaml.v2 v2.4.0
)

require golang.org/x/net v0.17.0 // indirect
```

### External Tools
- **Hauler CLI**: Integrated binary for all operations
- **Helm**: Used by Hauler for chart operations
- **javascript-obfuscator**: Build-time code obfuscation

## Infrastructure

### Container Base
- **Image**: Alpine Linux (latest)
- **Size**: Minimal footprint
- **Security**: Reduced attack surface

### Persistent Storage
- **Type**: Docker volumes
- **Paths**: `/data/store`, `/data/manifests`, `/data/hauls`, `/data/config`
- **Permissions**: Configurable (0600 for sensitive files)

## Development Commands

### Building

```bash
# Build Docker image
docker build -t hauler-ui:latest .

# Build with specific tag
docker build -t hauler-ui:v3.3.5 .

# Build without cache
docker build --no-cache -t hauler-ui:latest .
```

### Running

```bash
# Start with Docker Compose
docker compose up -d

# Start with logs
docker compose up

# Stop
docker compose down

# Restart
docker compose restart
```

### Development

```bash
# Run Go backend locally
cd backend
go run main.go

# Install Go dependencies
go mod download

# Format Go code
go fmt ./...

# Run Go tests
go test ./...
```

### Testing

```bash
# Comprehensive test suite
./tests/comprehensive_test_suite.sh

# Security scan
./tests/security_scan.sh

# Agent tests
./tests/run_agent_tests.sh

# All tests
./tests/run_all_tests.sh
```

### Maintenance

```bash
# View logs
docker compose logs -f

# View backend logs only
docker compose logs -f hauler-ui

# Execute shell in container
docker compose exec hauler-ui sh

# Clean up volumes
docker compose down -v
```

## API Endpoints

### Store Operations
- `POST /api/store/add/chart` - Add Helm chart
- `POST /api/store/add/image` - Add container image
- `POST /api/store/add/file` - Add file
- `POST /api/store/sync` - Sync from manifest
- `POST /api/store/save` - Save to haul
- `POST /api/store/load` - Load from haul
- `POST /api/store/copy` - Copy to registry
- `POST /api/store/serve/start` - Start serve mode
- `POST /api/store/serve/stop` - Stop serve mode
- `GET /api/store/serve/status` - Get serve status
- `GET /api/store/info` - Get store info
- `POST /api/store/extract` - Extract artifacts
- `DELETE /api/store/remove` - Remove artifacts
- `DELETE /api/store/clear` - Clear store

### Repository Operations
- `POST /api/repositories/add` - Add repository
- `DELETE /api/repositories/{name}` - Remove repository
- `GET /api/repositories` - List repositories
- `GET /api/repositories/{name}/charts` - Browse charts

### Registry Operations
- `POST /api/registry/login` - Registry login
- `POST /api/registry/logout` - Registry logout
- `POST /api/registry/push` - Push to registry

### File Operations
- `POST /api/files/upload/manifest` - Upload manifest
- `POST /api/files/upload/key` - Upload Cosign key
- `POST /api/files/upload/cert` - Upload certificate
- `POST /api/files/upload/values` - Upload values file
- `GET /api/files/manifests` - List manifests
- `GET /api/files/keys` - List keys
- `GET /api/files/certs` - List certificates
- `GET /api/files/values` - List values files
- `DELETE /api/files/{type}/{filename}` - Delete file

### WebSocket
- `WS /ws/logs` - Real-time log streaming

## Environment Variables

```bash
HAULER_STORE=/data/store  # Store location
```

## Port Configuration

- **8080**: HTTP server (UI and API)
- **5000**: Serve mode (registry and fileserver)

## Version Information

- **Project Version**: 3.3.5
- **Go Version**: 1.21
- **Docker Compose Version**: 3.8
- **License**: Apache 2.0
