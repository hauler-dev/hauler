# API Reference

Complete reference for all Hauler UI API endpoints.

## Base URL

```
http://localhost:8080/api
```

## Authentication

Currently no authentication required. Authentication system planned for v3.4.0.

## Response Format

All endpoints return JSON with this structure:

```json
{
  "success": true,
  "output": "Operation result",
  "error": "Error message (if success=false)"
}
```

## Endpoints

### Health Check

**GET** `/health`

Check if the server is running.

**Response:**
```json
{
  "healthy": true
}
```

---

### Store Operations

#### Get Store Info

**GET** `/store/info`

Retrieve current store statistics and content listing.

**Response:**
```json
{
  "success": true,
  "output": "Store information..."
}
```

#### Add Content

**POST** `/store/add-content`

Add charts or images to the store.

**Request Body:**
```json
{
  "type": "chart",
  "name": "nginx",
  "version": "1.0.0",
  "repository": "https://charts.bitnami.com/bitnami",
  "platform": "linux/amd64",
  "addImages": true,
  "addDependencies": true
}
```

**Parameters:**
- `type` (string): "chart" or "image"
- `name` (string): Chart/image name
- `version` (string): Version (optional for images)
- `repository` (string): Repository URL (for charts)
- `platform` (string): Target platform
- `addImages` (boolean): Extract images from chart
- `addDependencies` (boolean): Include dependencies

#### Sync Store

**POST** `/store/sync`

Sync store from a manifest file.

**Request Body:**
```json
{
  "filename": "manifest.yaml",
  "platform": "linux/amd64",
  "key": "cosign.pub"
}
```

#### Save Haul

**POST** `/store/save`

Create a haul archive from store contents.

**Request Body:**
```json
{
  "filename": "my-haul.tar.zst",
  "platform": "linux/amd64"
}
```

#### Load Haul

**POST** `/store/load`

Load a haul archive into the store.

**Request Body:**
```json
{
  "filename": "my-haul.tar.zst"
}
```

#### Clear Store

**POST** `/store/clear`

Remove all content from the store.

**Response:**
```json
{
  "success": true,
  "output": "Removing 10 artifacts...\n✓ Removed"
}
```

---

### Repository Management

#### Add Repository

**POST** `/repos/add`

Add a Helm chart repository.

**Request Body:**
```json
{
  "name": "bitnami",
  "url": "https://charts.bitnami.com/bitnami"
}
```

#### List Repositories

**GET** `/repos/list`

Get all configured repositories.

**Response:**
```json
{
  "repositories": [
    {
      "name": "bitnami",
      "url": "https://charts.bitnami.com/bitnami"
    }
  ]
}
```

#### Remove Repository

**DELETE** `/repos/remove/{name}`

Remove a repository by name.

#### Browse Repository Charts

**GET** `/repos/charts/{name}`

Get all charts and versions from a repository.

**Response:**
```json
{
  "charts": {
    "nginx": ["1.0.0", "1.0.1"],
    "redis": ["2.0.0"]
  },
  "details": {
    "nginx": {
      "name": "nginx",
      "version": "1.0.1",
      "description": "NGINX web server",
      "repository": "https://charts.bitnami.com/bitnami"
    }
  }
}
```

---

### Registry Operations

#### Configure Registry

**POST** `/registry/configure`

Add or update registry configuration.

**Request Body:**
```json
{
  "name": "my-registry",
  "url": "registry.example.com",
  "username": "user",
  "password": "pass",
  "insecure": false
}
```

#### List Registries

**GET** `/registry/list`

Get all configured registries (passwords masked).

#### Remove Registry

**DELETE** `/registry/remove/{name}`

Remove a registry configuration.

#### Login to Registry

**POST** `/registry/login`

Authenticate with a registry.

**Request Body:**
```json
{
  "registry": "registry.example.com",
  "username": "user",
  "password": "pass"
}
```

#### Logout from Registry

**POST** `/registry/logout`

Remove registry authentication.

**Request Body:**
```json
{
  "registry": "registry.example.com"
}
```

#### Push to Registry

**POST** `/registry/push`

Push store contents to a registry.

**Request Body:**
```json
{
  "registryName": "my-registry",
  "plainHttp": false,
  "only": "charts"
}
```

---

### File Operations

#### Upload File

**POST** `/files/upload`

Upload a manifest or haul file.

**Form Data:**
- `file`: File to upload
- `type`: "manifest" or "haul"

#### List Files

**GET** `/files/list?type={type}`

List uploaded files.

**Parameters:**
- `type`: "manifest" or "haul"

#### Download File

**GET** `/files/download/{filename}?type={type}`

Download a file.

#### Delete File

**DELETE** `/files/delete/{filename}?type={type}`

Delete a file.

---

### Serve Mode

#### Start Server

**POST** `/serve/start`

Start registry or fileserver.

**Request Body:**
```json
{
  "port": "5000",
  "mode": "registry",
  "readonly": true,
  "tlsCert": "server.crt",
  "tlsKey": "server.key"
}
```

#### Stop Server

**POST** `/serve/stop`

Stop the running server.

#### Server Status

**GET** `/serve/status`

Check if server is running.

**Response:**
```json
{
  "running": true
}
```

---

### WebSocket

#### Live Logs

**WebSocket** `/logs`

Connect to receive real-time command output.

**Example:**
```javascript
const ws = new WebSocket('ws://localhost:8080/api/logs');
ws.onmessage = (e) => console.log(e.data);
```

---

## Error Codes

- `200` - Success
- `400` - Bad Request (invalid parameters)
- `404` - Not Found (resource doesn't exist)
- `500` - Internal Server Error

## Rate Limiting

No rate limiting currently implemented. Planned for v3.4.0.

## Examples

### cURL Examples

**Add a chart:**
```bash
curl -X POST http://localhost:8080/api/store/add-content \
  -H "Content-Type: application/json" \
  -d '{
    "type": "chart",
    "name": "nginx",
    "repository": "https://charts.bitnami.com/bitnami"
  }'
```

**Get store info:**
```bash
curl http://localhost:8080/api/store/info
```

### JavaScript Examples

**Using Fetch API:**
```javascript
const response = await fetch('/api/store/info');
const data = await response.json();
console.log(data.output);
```

**Upload file:**
```javascript
const formData = new FormData();
formData.append('file', fileInput.files[0]);
formData.append('type', 'manifest');

const response = await fetch('/api/files/upload', {
  method: 'POST',
  body: formData
});
```
