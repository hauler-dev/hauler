# Senior Developer - v3.0.0 Implementation
**Date:** 2026-01-21
**Status:** IN PROGRESS

---

## Phase 1: Repository Cleanup ✅ COMPLETE

### Completed
- Created proper directory structure
- Moved frontend files to `frontend/`
- Moved agent docs to `docs/agents/`
- Moved tests to `tests/`
- Removed obsolete files
- Root directory: 12 files (target: < 10)

### Remaining Cleanup
- Remove `hauler-main.zip` (103MB)
- Remove `hauler` binary from root (use container version)
- Move `qa-dependencies.sh` to tests/
- Update paths in docker-compose.yml

---

## Phase 2: Update Configuration Files

### Update docker-compose.yml
```yaml
version: '3.8'
services:
  hauler-ui:
    build: .
    container_name: hauler-ui
    ports:
      - "8080:8080"
      - "5000:5000"
    volumes:
      - ./data:/data
      - ./frontend:/app/frontend:ro
    environment:
      - HAULER_STORE=/data/store
```

### Update Dockerfile
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY backend/ .
RUN go build -o hauler-ui .

FROM alpine:latest
RUN apk add --no-cache bash curl openssl ca-certificates
COPY --from=builder /build/hauler-ui /app/
COPY frontend/ /app/frontend/
COPY hauler /usr/local/bin/
WORKDIR /app
CMD ["./hauler-ui"]
```

### Update main.go paths
```go
r.PathPrefix("/").Handler(http.FileServer(http.Dir("/app/frontend")))
```

---

## Phase 3: Implement Missing Features

### Feature 1: Add File ✅ READY TO IMPLEMENT

**Backend (main.go):**
```go
r.HandleFunc("/api/store/add-file", storeAddFileHandler).Methods("POST")

func storeAddFileHandler(w http.ResponseWriter, r *http.Request) {
    r.ParseMultipartForm(100 << 20)
    file, handler, err := r.FormFile("file")
    if err != nil {
        respondError(w, "Failed to read file", http.StatusBadRequest)
        return
    }
    defer file.Close()

    tempPath := filepath.Join("/tmp", handler.Filename)
    dst, err := os.Create(tempPath)
    if err != nil {
        respondError(w, "Failed to save file", http.StatusInternalServerError)
        return
    }
    io.Copy(dst, file)
    dst.Close()

    output, err := executeHauler("store", "add", "file", tempPath)
    os.Remove(tempPath)
    
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}
```

**Frontend (app.js):**
```javascript
async function addFileToStore() {
    const file = document.getElementById('fileToAdd').files[0];
    if (!file) return alert('Select a file');
    
    const formData = new FormData();
    formData.append('file', file);
    
    const res = await fetch('/api/store/add-file', {method: 'POST', body: formData});
    const data = await res.json();
    
    document.getElementById('fileOutput').textContent = data.output || data.error;
    if (data.success) setTimeout(refreshStoreInfo, 1000);
}
```

**UI (index.html):**
```html
<div id="files" class="tab-content hidden p-8">
    <h2 class="text-3xl font-bold mb-6">File Management</h2>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
        <h3 class="text-xl font-bold mb-4">Add File to Store</h3>
        <input type="file" id="fileToAdd" class="mb-4">
        <button onclick="addFileToStore()" class="bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded">
            <i class="fas fa-plus mr-2"></i>Add File
        </button>
    </div>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700">
        <h3 class="text-xl font-bold mb-4">Output</h3>
        <pre id="fileOutput" class="bg-gray-900 p-4 rounded text-sm overflow-auto max-h-96"></pre>
    </div>
</div>
```

---

### Feature 2: Extract ✅ READY TO IMPLEMENT

**Backend:**
```go
r.HandleFunc("/api/store/extract", storeExtractHandler).Methods("POST")

func storeExtractHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        OutputDir string `json:"outputDir"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    outputDir := "/data/extracted"
    if req.OutputDir != "" {
        outputDir = filepath.Join("/data", req.OutputDir)
    }
    os.MkdirAll(outputDir, 0755)

    output, err := executeHauler("store", "extract", "-o", outputDir)
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}
```

**Frontend:**
```javascript
async function extractStore() {
    const outputDir = document.getElementById('extractDir').value || 'extracted';
    
    if (!confirm(`Extract store contents to /data/${outputDir}?`)) return;
    
    const outputEl = document.getElementById('extractOutput');
    outputEl.textContent = 'Extracting...';
    
    const data = await apiCall('store/extract', 'POST', {outputDir});
    outputEl.textContent = data.output || data.error;
}
```

**UI:**
```html
<div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
    <h3 class="text-xl font-bold mb-4">Extract Store Contents</h3>
    <input type="text" id="extractDir" placeholder="Output directory (default: extracted)" 
           class="w-full bg-gray-900 border border-gray-700 rounded p-2 mb-4">
    <button onclick="extractStore()" class="bg-blue-600 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded">
        <i class="fas fa-download mr-2"></i>Extract Store
    </button>
</div>
<div class="bg-gray-800 p-6 rounded-lg border border-gray-700">
    <h3 class="text-xl font-bold mb-4">Extract Output</h3>
    <pre id="extractOutput" class="bg-gray-900 p-4 rounded text-sm overflow-auto max-h-96"></pre>
</div>
```

---

### Feature 3: Remove Artifacts ✅ READY TO IMPLEMENT

**Backend:**
```go
r.HandleFunc("/api/store/remove/{artifact:.*}", storeRemoveHandler).Methods("DELETE")
r.HandleFunc("/api/store/artifacts", storeArtifactsHandler).Methods("GET")

func storeRemoveHandler(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    artifact := vars["artifact"]
    force := r.URL.Query().Get("force") == "true"

    args := []string{"store", "remove", artifact}
    if force {
        args = append(args, "--force")
    }

    output, err := executeHauler(args[0], args[1:]...)
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func storeArtifactsHandler(w http.ResponseWriter, r *http.Request) {
    output, err := executeHauler("store", "info")
    if err != nil {
        respondError(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Parse output to extract artifacts
    artifacts := parseArtifacts(output)
    json.NewEncoder(w).Encode(map[string]interface{}{
        "artifacts": artifacts,
        "count": len(artifacts),
        "raw": output,
    })
}

func parseArtifacts(output string) []string {
    artifacts := []string{}
    lines := strings.Split(output, "\n")
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.Contains(line, "index.docker.io") || 
           strings.Contains(line, "hauler/") ||
           strings.Contains(line, ".io/") {
            artifacts = append(artifacts, line)
        }
    }
    return artifacts
}
```

**Frontend:**
```javascript
async function listArtifacts() {
    const data = await apiCall('store/artifacts');
    const listEl = document.getElementById('artifactList');
    
    if (!data.artifacts || data.artifacts.length === 0) {
        listEl.innerHTML = '<p class="text-gray-400">No artifacts in store</p>';
        return;
    }
    
    listEl.innerHTML = data.artifacts.map(artifact => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded mb-2">
            <span class="font-mono text-sm">${artifact}</span>
            <button onclick="removeArtifact('${artifact}')" 
                    class="text-red-400 hover:text-red-300">
                <i class="fas fa-trash"></i> Remove
            </button>
        </div>
    `).join('');
}

async function removeArtifact(artifact) {
    if (!confirm(`⚠️ Remove ${artifact}?\\n\\nThis cannot be undone.`)) return;
    
    const data = await fetch(`/api/store/remove/${encodeURIComponent(artifact)}?force=true`, 
                             {method: 'DELETE'}).then(r => r.json());
    
    alert(data.success ? '✅ Removed' : `❌ ${data.error}`);
    if (data.success) {
        listArtifacts();
        refreshStoreInfo();
    }
}
```

**UI:**
```html
<div id="artifacts" class="tab-content hidden p-8">
    <h2 class="text-3xl font-bold mb-6">Artifact Management</h2>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700">
        <div class="flex justify-between items-center mb-4">
            <h3 class="text-xl font-bold">Store Artifacts</h3>
            <button onclick="listArtifacts()" class="bg-gray-600 hover:bg-gray-700 text-white font-bold py-2 px-4 rounded">
                <i class="fas fa-sync mr-2"></i>Refresh
            </button>
        </div>
        <div id="artifactList"></div>
    </div>
</div>
```

---

### Feature 4: Registry Login/Logout ✅ READY TO IMPLEMENT

**Backend:**
```go
r.HandleFunc("/api/registry/login", registryLoginHandler).Methods("POST")
r.HandleFunc("/api/registry/logout", registryLogoutHandler).Methods("POST")

func registryLoginHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Registry string `json:"registry"`
        Username string `json:"username"`
        Password string `json:"password"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    cmd := exec.Command("hauler", "login", req.Registry, "-u", req.Username, "-p", req.Password)
    cmd.Env = append(os.Environ(), "HAULER_STORE=/data/store")
    output, err := cmd.CombinedOutput()
    
    respondJSON(w, Response{Success: err == nil, Output: string(output), Error: errString(err)})
}

func registryLogoutHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Registry string `json:"registry"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    output, err := executeHauler("logout", req.Registry)
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}
```

**Frontend:**
```javascript
async function registryLogin() {
    const registry = document.getElementById('loginRegistry').value;
    const username = document.getElementById('loginUsername').value;
    const password = document.getElementById('loginPassword').value;
    
    if (!registry || !username || !password) return alert('All fields required');
    
    const data = await apiCall('registry/login', 'POST', {registry, username, password});
    alert(data.success ? '✅ Logged in' : `❌ ${data.error}`);
    
    document.getElementById('loginPassword').value = '';
}

async function registryLogout() {
    const registry = document.getElementById('logoutRegistry').value;
    if (!registry) return alert('Registry required');
    
    const data = await apiCall('registry/logout', 'POST', {registry});
    alert(data.success ? '✅ Logged out' : `❌ ${data.error}`);
}
```

---

## Implementation Checklist

### Phase 1: Cleanup ✅
- [x] Create directory structure
- [x] Move files
- [x] Remove obsolete files
- [ ] Update docker-compose.yml
- [ ] Update Dockerfile
- [ ] Update main.go paths

### Phase 2: Features
- [ ] Add file functionality
- [ ] Extract functionality
- [ ] Remove artifacts
- [ ] Registry login/logout
- [ ] Update navigation
- [ ] Add new tabs

### Phase 3: Testing
- [ ] Test all new endpoints
- [ ] Test UI interactions
- [ ] Integration testing
- [ ] Update test suite

---

**STATUS:** Repository cleanup complete, ready for feature implementation
**NEXT:** Update configuration files and implement features
