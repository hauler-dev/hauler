# Project Structure

## Directory Organization

```
hauler-ui/
├── backend/              # Go backend server
│   ├── main.go          # 37 REST API endpoints + WebSocket server
│   ├── go.mod           # Go module dependencies
│   └── go.sum           # Dependency checksums
├── frontend/            # Web UI assets
│   ├── index.html       # Single-page application
│   ├── app.js           # JavaScript (obfuscated in production)
│   ├── tailwind.min.js  # Tailwind CSS framework
│   ├── fontawesome.min.css  # Icon library
│   └── webfonts/        # Font Awesome web fonts
├── mcp_server/          # Model Context Protocol integration
│   ├── mcp-command-server.py  # MCP server for AI assistant integration
│   ├── mcp-config.json  # MCP configuration
│   └── README.md        # MCP documentation
├── docs/                # Documentation
│   ├── agents/          # Multi-agent development artifacts (35 files)
│   ├── FEATURES.md      # Feature documentation
│   ├── SECURITY.md      # Security guidelines
│   ├── TESTING.md       # Test documentation
│   └── UI_README.md     # UI walkthrough
├── tests/               # Test suites
│   ├── comprehensive_test_suite.sh  # Full test suite
│   ├── security_scan.sh # Security scanning
│   ├── run_agent_tests.sh  # Agent-based tests
│   └── reports/         # Test results
├── data/                # Persistent storage (mounted volumes)
│   ├── store/           # OCI artifact store
│   ├── manifests/       # Hauler manifest files
│   ├── hauls/           # Compressed haul archives
│   └── config/          # Keys, certificates, values files
├── Dockerfile           # Multi-stage build with obfuscation
├── Dockerfile.security  # Security scanning container
├── docker-compose.yml   # Container orchestration
├── Makefile             # Build automation
└── README.md            # Project documentation
```

## Core Components

### Backend (Go)
**File**: `backend/main.go`  
**Purpose**: HTTP server and Hauler CLI integration  
**Key Responsibilities**:
- 37 REST API endpoints for all Hauler operations
- WebSocket server for real-time log streaming
- File upload handling (manifests, keys, certificates, values)
- Command execution and output capture
- Static file serving for frontend

**API Categories**:
- Store operations (add, sync, save, load, copy, serve, extract, remove, info)
- Repository management (add, remove, list, browse charts)
- Registry operations (login, logout, push)
- File operations (upload, list, delete)
- Serve control (start, stop, status)

### Frontend (JavaScript)
**File**: `frontend/app.js`  
**Purpose**: Single-page web application  
**Key Responsibilities**:
- Tab-based navigation (Store, Repositories, Push to Registry, Serve)
- Dynamic form generation for Hauler commands
- Real-time log display via WebSocket
- File upload handling
- Interactive chart browser with batch selection

**UI Components**:
- Store management interface
- Repository browser with chart selection
- Registry configuration and push interface
- Serve mode controls
- Live log viewer

### MCP Server (Python)
**File**: `mcp_server/mcp-command-server.py`  
**Purpose**: Model Context Protocol integration  
**Key Responsibilities**:
- Expose Hauler commands via MCP protocol
- Enable AI assistant integration
- Command execution and result formatting

## Architectural Patterns

### Three-Tier Architecture
```
┌─────────────────────────────────────┐
│  Presentation Layer (Browser)       │
│  - HTML/CSS (Tailwind)              │
│  - JavaScript (Obfuscated)          │
│  - WebSocket Client                 │
└─────────────────────────────────────┘
              ↓ HTTP/WS
┌─────────────────────────────────────┐
│  Application Layer (Go Backend)     │
│  - REST API (Gorilla Mux)           │
│  - WebSocket Server                 │
│  - File Upload Handler              │
└─────────────────────────────────────┘
              ↓ exec
┌─────────────────────────────────────┐
│  Integration Layer (Hauler CLI)     │
│  - store add/sync/save/load         │
│  - store copy/serve/extract         │
│  - login/logout                     │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│  Persistence Layer                  │
│  - /data/store (OCI artifacts)      │
│  - /data/manifests (YAML files)     │
│  - /data/hauls (tar.zst archives)   │
│  - /data/config (keys, certs)       │
└─────────────────────────────────────┘
```

### Communication Patterns

**HTTP REST API**:
- Client → Backend: JSON requests for operations
- Backend → Client: JSON responses with status/results
- Used for: All CRUD operations, configuration, file uploads

**WebSocket**:
- Client ← Backend: Real-time log streaming
- Used for: Live command output, progress updates

**Process Execution**:
- Backend → Hauler CLI: Command-line execution
- Hauler CLI → Backend: stdout/stderr capture
- Used for: All Hauler operations

## Component Relationships

```
┌──────────────┐
│   Browser    │
└──────┬───────┘
       │ HTTP/WS
       ↓
┌──────────────┐     ┌──────────────┐
│  Go Backend  │────→│  Hauler CLI  │
└──────┬───────┘     └──────┬───────┘
       │                    │
       ↓                    ↓
┌──────────────┐     ┌──────────────┐
│  Frontend    │     │  Data Store  │
│   Assets     │     │  (Volumes)   │
└──────────────┘     └──────────────┘
```

## Data Flow

### Adding Content
1. User selects charts/images in UI
2. Frontend sends POST to `/api/store/add/*`
3. Backend constructs Hauler CLI command
4. Hauler CLI downloads and stores content
5. Backend streams logs via WebSocket
6. Frontend displays real-time progress

### Saving Haul
1. User clicks "Save to Haul"
2. Frontend sends POST to `/api/store/save`
3. Backend executes `hauler store save`
4. Haul file created in `/data/hauls`
5. Backend returns download URL
6. Frontend triggers file download

### Browsing Charts
1. User clicks "Browse" on repository
2. Frontend sends GET to `/api/repositories/{name}/charts`
3. Backend executes `helm search repo`
4. Backend parses and returns chart list
5. Frontend displays interactive chart browser
6. User selects charts for batch addition

## Deployment Architecture

### Docker Container
- **Base Image**: Alpine Linux (minimal)
- **Build**: Multi-stage (Go build → JavaScript obfuscation → final image)
- **Ports**: 8080 (HTTP), 5000 (serve mode)
- **Volumes**: 4 persistent volumes for data
- **Health Check**: HTTP endpoint monitoring

### Persistent Storage
- `/data/store`: OCI artifact storage (Hauler store)
- `/data/manifests`: User-uploaded manifest files
- `/data/hauls`: Generated haul archives
- `/data/config`: Keys, certificates, values files

## Security Layers

1. **Frontend**: JavaScript obfuscation (control flow flattening, string encoding)
2. **Backend**: Input validation, secure file handling
3. **Container**: Minimal base image, no unnecessary packages
4. **Network**: Configurable ports, TLS support
5. **Storage**: File permissions (0600 for sensitive files)
