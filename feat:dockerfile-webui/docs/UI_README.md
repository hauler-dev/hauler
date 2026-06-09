# Hauler UI

Web-based interface for Rancher Government Hauler - Airgap Swiss Army Knife

## Features

- **Dashboard**: Real-time store status and health monitoring
- **Store Management**: Sync, save, load, and manage Hauler stores
- **Manifest Editor**: Upload and manage YAML manifests
- **Haul Management**: Upload, download, and manage haul archives
- **Registry Server**: Start/stop embedded registry and fileserver
- **CA Certificate**: Upload custom CA certificates for secure registries
- **Live Logs**: Real-time command output via WebSocket
- **Persistent Storage**: All data persists across container restarts

## Quick Start

### Using Docker Compose (Recommended)

```bash
docker-compose up -d
```

Access the UI at `http://localhost:8080`

### Using Docker

```bash
# Build
docker build -t hauler-ui .

# Run
docker run -d \
  -p 8080:8080 \
  -p 5000:5000 \
  -v $(pwd)/data/store:/data/store \
  -v $(pwd)/data/manifests:/data/manifests \
  -v $(pwd)/data/hauls:/data/hauls \
  -v $(pwd)/data/config:/data/config \
  --name hauler-ui \
  hauler-ui
```

## Architecture

- **Frontend**: Vanilla JavaScript + Tailwind CSS
- **Backend**: Go API wrapping Hauler CLI
- **Container**: Alpine Linux with Hauler installed
- **Communication**: REST API + WebSocket for logs

## Volumes

- `/data/store` - Hauler store data
- `/data/manifests` - YAML manifest files
- `/data/hauls` - Haul archive files (.tar.zst)
- `/data/config` - Configuration and certificates

## Ports

- `8080` - Web UI
- `5000` - Hauler registry/fileserver (configurable)

## Usage

### Store Operations

1. **Sync**: Upload a manifest and sync content to store
2. **Save**: Export store to a haul archive
3. **Load**: Import haul archive into store
4. **Info**: View store contents and statistics

### Serve Registry

1. Navigate to "Serve" tab
2. Configure port (default: 5000)
3. Click "Start Server"
4. Registry available at `http://localhost:5000`

### CA Certificate

1. Navigate to "Settings" tab
2. Upload `.crt` or `.pem` certificate
3. Certificate automatically installed in container

## Development

### Build Backend

```bash
cd backend
go mod download
go build -o hauler-ui main.go
```

### Run Locally

```bash
export HAULER_STORE=./data/store
./backend/hauler-ui
```

## Security

- Input validation on all API endpoints
- No shell injection vulnerabilities
- Secure file upload handling
- CA certificate validation
- Credential storage in persistent volumes

## API Endpoints

- `GET /api/health` - Health check
- `GET /api/store/info` - Store information
- `POST /api/store/sync` - Sync from manifest
- `POST /api/store/save` - Save to haul
- `POST /api/store/load` - Load from haul
- `POST /api/files/upload` - Upload files
- `GET /api/files/list` - List files
- `POST /api/cert/upload` - Upload CA cert
- `POST /api/serve/start` - Start registry
- `POST /api/serve/stop` - Stop registry
- `GET /api/serve/status` - Server status
- `WS /api/logs` - Live logs stream

## Troubleshooting

### Container won't start
```bash
docker logs hauler-ui
```

### Permission issues
```bash
chmod -R 755 data/
```

### Reset everything
```bash
docker-compose down -v
rm -rf data/
docker-compose up -d
```

## License

See parent project LICENSE
