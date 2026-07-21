# Senior Developer - Implementation Guide
**Date:** 2024
**Version:** 2.1.0
**Features:** System Reset & Private Registry Push

---

## Implementation Overview

Implementing two features with minimal, production-ready code:
1. **System Reset** - Simple, safe store clearing
2. **Registry Push** - Harbor/private registry integration

---

## FEATURE 1: SYSTEM RESET

### Backend Implementation

#### File: `backend/main.go`

**Add endpoint registration:**
```go
r.HandleFunc("/api/system/reset", systemResetHandler).Methods("POST")
```

**Add handler function:**
```go
func systemResetHandler(w http.ResponseWriter, r *http.Request) {
    output, err := executeHauler("store", "clear")
    if err != nil {
        respondJSON(w, Response{Success: false, Output: output, Error: err.Error()})
        return
    }
    respondJSON(w, Response{Success: true, Output: "System reset successfully"})
}
```

### Frontend Implementation

#### File: `static/app.js`

**Add reset function:**
```javascript
async function resetSystem() {
    if (!confirm('⚠️ WARNING: Reset Hauler System?\\n\\nThis will clear the entire store.\\nUploaded files will be preserved.\\n\\nThis action cannot be undone.')) return;
    
    if (!confirm('⚠️ FINAL CONFIRMATION\\n\\nAre you absolutely sure?')) return;
    
    const outputEl = document.getElementById('settingsOutput');
    outputEl.textContent = 'Resetting system...';
    
    const data = await apiCall('system/reset', 'POST');
    outputEl.textContent = data.output || data.error;
    
    if (data.success) {
        setTimeout(refreshStoreInfo, 1000);
    }
}
```

#### File: `static/index.html`

**Add to Settings tab (after CA Certificate section):**
```html
<div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
    <h3 class="text-xl font-bold mb-4 text-red-400">
        <i class="fas fa-exclamation-triangle mr-2"></i>Danger Zone
    </h3>
    <p class="text-gray-400 mb-4">Reset the Hauler system to clean state. This clears the store but preserves uploaded files.</p>
    <button onclick="resetSystem()" class="bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded">
        <i class="fas fa-redo mr-2"></i>Reset Hauler System
    </button>
</div>
<div class="bg-gray-800 p-6 rounded-lg border border-gray-700">
    <h3 class="text-xl font-bold mb-4">System Output</h3>
    <pre id="settingsOutput" class="bg-gray-900 p-4 rounded text-sm overflow-auto max-h-64"></pre>
</div>
```

---

## FEATURE 2: PRIVATE REGISTRY PUSH

### Backend Implementation

#### File: `backend/main.go`

**Add data structures:**
```go
type RegistryConfig struct {
    Name     string `json:"name"`
    URL      string `json:"url"`
    Username string `json:"username"`
    Password string `json:"password"`
    Insecure bool   `json:"insecure"`
}

type PushRequest struct {
    RegistryName string   `json:"registryName"`
    Content      []string `json:"content"`
}

var (
    registries    = make(map[string]RegistryConfig)
    registriesMux sync.RWMutex
)
```

**Add endpoint registrations:**
```go
r.HandleFunc("/api/registry/configure", registryConfigureHandler).Methods("POST")
r.HandleFunc("/api/registry/list", registryListHandler).Methods("GET")
r.HandleFunc("/api/registry/remove/{name}", registryRemoveHandler).Methods("DELETE")
r.HandleFunc("/api/registry/test", registryTestHandler).Methods("POST")
r.HandleFunc("/api/registry/push", registryPushHandler).Methods("POST")
```

**Add handler functions:**
```go
func loadRegistries() {
    regFile := "/data/config/registries.json"
    data, err := os.ReadFile(regFile)
    if err != nil {
        return
    }
    json.Unmarshal(data, &registries)
}

func saveRegistries() error {
    regFile := "/data/config/registries.json"
    os.MkdirAll(filepath.Dir(regFile), 0755)
    data, err := json.Marshal(registries)
    if err != nil {
        return err
    }
    return os.WriteFile(regFile, data, 0600)
}

func registryConfigureHandler(w http.ResponseWriter, r *http.Request) {
    var reg RegistryConfig
    if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
        respondError(w, "Invalid request", http.StatusBadRequest)
        return
    }

    registriesMux.Lock()
    registries[reg.Name] = reg
    registriesMux.Unlock()

    if err := saveRegistries(); err != nil {
        respondError(w, "Failed to save registry", http.StatusInternalServerError)
        return
    }

    respondJSON(w, Response{Success: true, Output: "Registry configured successfully"})
}

func registryListHandler(w http.ResponseWriter, r *http.Request) {
    registriesMux.RLock()
    defer registriesMux.RUnlock()

    regList := make([]RegistryConfig, 0, len(registries))
    for _, reg := range registries {
        safeCopy := reg
        safeCopy.Password = "***"
        regList = append(regList, safeCopy)
    }

    json.NewEncoder(w).Encode(map[string]interface{}{"registries": regList})
}

func registryRemoveHandler(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    name := vars["name"]

    registriesMux.Lock()
    delete(registries, name)
    registriesMux.Unlock()

    if err := saveRegistries(); err != nil {
        respondError(w, "Failed to save registries", http.StatusInternalServerError)
        return
    }

    respondJSON(w, Response{Success: true, Output: "Registry removed successfully"})
}

func registryTestHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name string `json:"name"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    registriesMux.RLock()
    reg, exists := registries[req.Name]
    registriesMux.RUnlock()

    if !exists {
        respondError(w, "Registry not found", http.StatusNotFound)
        return
    }

    args := []string{"store", "copy", "--username", reg.Username, "--password", reg.Password}
    if reg.Insecure {
        args = append(args, "--insecure")
    }
    args = append(args, "registry://"+reg.URL)

    output, err := executeHauler(args[0], args[1:]...)
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func registryPushHandler(w http.ResponseWriter, r *http.Request) {
    var req PushRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, "Invalid request", http.StatusBadRequest)
        return
    }

    registriesMux.RLock()
    reg, exists := registries[req.RegistryName]
    registriesMux.RUnlock()

    if !exists {
        respondError(w, "Registry not found", http.StatusNotFound)
        return
    }

    args := []string{"store", "copy", "--username", reg.Username, "--password", reg.Password}
    if reg.Insecure {
        args = append(args, "--insecure")
    }
    args = append(args, "registry://"+reg.URL)

    output, err := executeHauler(args[0], args[1:]...)
    respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}
```

**Update main() to load registries:**
```go
func main() {
    loadRepositories()
    loadRegistries()  // Add this line
    // ... rest of main
}
```

### Frontend Implementation

#### File: `static/app.js`

**Add registry management functions:**
```javascript
async function configureRegistry() {
    const name = document.getElementById('registryName').value;
    const url = document.getElementById('registryURL').value;
    const username = document.getElementById('registryUsername').value;
    const password = document.getElementById('registryPassword').value;
    const insecure = document.getElementById('registryInsecure').checked;
    
    if (!name || !url) return alert('Name and URL are required');
    
    const data = await apiCall('registry/configure', 'POST', {
        name, url, username, password, insecure
    });
    
    alert(data.output || data.error);
    loadRegistries();
    
    document.getElementById('registryName').value = '';
    document.getElementById('registryURL').value = '';
    document.getElementById('registryUsername').value = '';
    document.getElementById('registryPassword').value = '';
    document.getElementById('registryInsecure').checked = false;
}

async function loadRegistries() {
    const data = await apiCall('registry/list');
    const listEl = document.getElementById('registryList');
    const selectEl = document.getElementById('pushRegistry');
    
    if (!data.registries || data.registries.length === 0) {
        listEl.innerHTML = '<p class="text-gray-400">No registries configured</p>';
        if (selectEl) selectEl.innerHTML = '<option value="">No registries available</option>';
        return;
    }
    
    listEl.innerHTML = data.registries.map(reg => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded">
            <div>
                <span class="font-bold">${reg.name}</span>
                <span class="text-gray-400 text-sm ml-2">${reg.url}</span>
            </div>
            <div class="flex gap-2">
                <button onclick="testRegistry('${reg.name}')" class="text-blue-400 hover:text-blue-300">
                    <i class="fas fa-plug"></i> Test
                </button>
                <button onclick="removeRegistry('${reg.name}')" class="text-red-400 hover:text-red-300">
                    <i class="fas fa-trash"></i>
                </button>
            </div>
        </div>
    `).join('');
    
    if (selectEl) {
        selectEl.innerHTML = '<option value="">Select registry...</option>' +
            data.registries.map(r => `<option value="${r.name}">${r.name}</option>`).join('');
    }
}

async function removeRegistry(name) {
    if (!confirm(`Remove registry ${name}?`)) return;
    await fetch(`/api/registry/remove/${name}`, { method: 'DELETE' });
    loadRegistries();
}

async function testRegistry(name) {
    const outputEl = document.getElementById('pushOutput');
    outputEl.textContent = `Testing connection to ${name}...`;
    
    const data = await apiCall('registry/test', 'POST', { name });
    outputEl.textContent = data.success ? 
        `✅ Connection successful to ${name}` : 
        `❌ Connection failed: ${data.error}`;
}

async function pushToRegistry() {
    const registryName = document.getElementById('pushRegistry').value;
    if (!registryName) return alert('Select a registry');
    
    if (!confirm(`Push all store content to ${registryName}?\\n\\nThis may take several minutes.`)) return;
    
    const outputEl = document.getElementById('pushOutput');
    outputEl.textContent = `Pushing to ${registryName}...`;
    
    const data = await apiCall('registry/push', 'POST', { 
        registryName,
        content: []
    });
    
    outputEl.textContent = data.output || data.error;
}
```

#### File: `static/index.html`

**Add new "Push" tab to navigation:**
```html
<button onclick="showTab('push')" class="nav-btn w-full text-left p-3 rounded mb-2 hover:bg-gray-700">
    <i class="fas fa-upload mr-2"></i> Push to Registry
</button>
```

**Add new "Push" tab content (before closing main tag):**
```html
<div id="push" class="tab-content hidden p-8">
    <h2 class="text-3xl font-bold mb-6">Push to Private Registry</h2>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
        <h3 class="text-xl font-bold mb-4">Configure Registry</h3>
        <div class="grid grid-cols-2 gap-4 mb-4">
            <input type="text" id="registryName" placeholder="Registry Name (e.g. harbor-prod)" class="bg-gray-900 border border-gray-700 rounded p-2">
            <input type="text" id="registryURL" placeholder="Registry URL (e.g. harbor.company.com)" class="bg-gray-900 border border-gray-700 rounded p-2">
            <input type="text" id="registryUsername" placeholder="Username" class="bg-gray-900 border border-gray-700 rounded p-2">
            <input type="password" id="registryPassword" placeholder="Password" class="bg-gray-900 border border-gray-700 rounded p-2">
        </div>
        <div class="mb-4">
            <label class="flex items-center">
                <input type="checkbox" id="registryInsecure" class="mr-2">
                <span class="text-gray-400">Allow insecure connection (skip TLS verification)</span>
            </label>
        </div>
        <button onclick="configureRegistry()" class="bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded">
            <i class="fas fa-plus mr-2"></i>Add Registry
        </button>
    </div>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
        <h3 class="text-xl font-bold mb-4">Configured Registries</h3>
        <div id="registryList" class="space-y-2"></div>
    </div>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700 mb-6">
        <h3 class="text-xl font-bold mb-4">Push Content</h3>
        <select id="pushRegistry" class="w-full bg-gray-900 border border-gray-700 rounded p-2 mb-4">
            <option value="">Select registry...</option>
        </select>
        <button onclick="pushToRegistry()" class="w-full bg-blue-600 hover:bg-blue-700 text-white font-bold py-3 px-4 rounded">
            <i class="fas fa-upload mr-2"></i>Push All Content to Registry
        </button>
    </div>
    
    <div class="bg-gray-800 p-6 rounded-lg border border-gray-700">
        <h3 class="text-xl font-bold mb-4">Output</h3>
        <pre id="pushOutput" class="bg-gray-900 p-4 rounded text-sm overflow-auto max-h-96">Ready to push content...</pre>
    </div>
</div>
```

**Update app.js initialization to load registries:**
```javascript
// At the end of app.js, update:
setInterval(updateServerStatus, 5000);
refreshStoreInfo();
updateServerStatus();
loadRepositories();
loadRegistries();  // Add this line
```

---

## Implementation Checklist

### System Reset
- [ ] Backend endpoint added
- [ ] Frontend function added
- [ ] UI button added to Settings
- [ ] Double confirmation implemented
- [ ] Output display added

### Registry Push
- [ ] Backend data structures added
- [ ] Registry CRUD endpoints added
- [ ] Registry storage implemented
- [ ] Frontend functions added
- [ ] UI tab created
- [ ] Configuration form added
- [ ] Test connection implemented
- [ ] Push functionality added

---

## Testing Instructions

### Test System Reset
```bash
1. Navigate to Settings tab
2. Click "Reset Hauler System"
3. Confirm both warnings
4. Verify store is cleared
5. Verify uploaded files remain
```

### Test Registry Push
```bash
1. Navigate to "Push to Registry" tab
2. Add Harbor registry configuration
3. Test connection
4. Push content
5. Verify content in Harbor
```

---

## Security Notes

1. **Credentials**: Stored in `/data/config/registries.json` with 0600 permissions
2. **Password Masking**: Passwords masked in UI list display
3. **No Logging**: Passwords never logged in executeHauler output
4. **TLS Option**: Insecure flag available for testing only

---

## Hauler Command Reference

### Reset
```bash
hauler store clear
```

### Push to Registry
```bash
hauler store copy \
  --username admin \
  --password secret \
  registry://harbor.company.com
```

---

**STATUS:** READY FOR IMPLEMENTATION
**ESTIMATED TIME:** 8 hours total
**PRIORITY:** HIGH

---

**IMPLEMENTATION BEGINS NOW**
