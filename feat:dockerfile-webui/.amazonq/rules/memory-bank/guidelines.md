# Development Guidelines

## Code Quality Standards

### Go Backend Standards

**File Structure:**
- Single main.go file containing all backend logic
- Package declaration: `package main`
- Imports organized in standard library, third-party, and local groups
- Global variables declared at package level with descriptive names

**Naming Conventions:**
- Exported types use PascalCase (e.g., `Repository`, `RegistryConfig`, `AddContentRequest`)
- Unexported variables use camelCase (e.g., `upgrader`, `logMux`, `repos`)
- HTTP handler functions end with `Handler` suffix (e.g., `healthHandler`, `commandHandler`)
- Constants use descriptive names matching their purpose

**Error Handling:**
- Always check errors immediately after operations
- Use helper functions for consistent error responses (`respondError`, `respondJSON`)
- Return early on errors to avoid nested conditionals
- Provide descriptive error messages with context

**Concurrency Patterns:**
- Use `sync.Mutex` for protecting shared state (e.g., `logMux`, `reposMux`, `registriesMux`)
- Use `sync.RWMutex` for read-heavy operations (e.g., `reposMux.RLock()`)
- Always defer unlock operations immediately after lock
- Use goroutines for background tasks (e.g., `go func() { serveCmd.Run() }()`)

**HTTP Patterns:**
- Use Gorilla Mux for routing with clear path patterns
- RESTful endpoint naming (e.g., `/api/store/info`, `/api/repos/add`)
- HTTP methods match operations (GET for reads, POST for creates, DELETE for removes)
- Always set appropriate Content-Type headers
- Use path variables for resource identifiers (e.g., `{name}`, `{filename}`)

**Data Structures:**
- Use structs with JSON tags for API request/response types
- Embed common fields in base types when appropriate
- Use maps for in-memory storage with appropriate locking
- Define clear types for domain concepts (Repository, RegistryConfig, etc.)

### JavaScript Frontend Standards

**Code Organization:**
- Global variables declared at top (e.g., `let ws`, `let manifestContent`)
- Functions organized by feature area
- Event handlers use descriptive names matching their purpose
- Async/await preferred over promise chains

**Function Patterns:**
- Async functions for API calls (e.g., `async function apiCall()`)
- Pure functions for data transformation (e.g., `updateChartPreview()`)
- Event handlers accept event parameter when needed
- Helper functions for repeated operations

**API Communication:**
- Centralized `apiCall` function for HTTP requests
- Consistent error handling with user-friendly messages
- Use `fetch` API with proper headers
- FormData for file uploads

**DOM Manipulation:**
- Use `document.getElementById()` for element selection
- Template literals for HTML generation
- Event delegation where appropriate
- Clear separation between data and presentation

**State Management:**
- Global state variables for application-wide data
- Local state in function scope when appropriate
- State updates trigger UI updates
- No framework-specific patterns (vanilla JavaScript)

### Python MCP Server Standards

**Class Structure:**
- Single class per file with clear responsibility
- `__init__` method for initialization
- Async methods for I/O operations
- Clear method naming describing actions

**Async Patterns:**
- Use `async def` for asynchronous operations
- `await` for async calls
- Proper error handling in async context
- JSON-RPC protocol implementation

**Error Handling:**
- Try-except blocks for error-prone operations
- Descriptive error messages
- Proper error response formatting
- Timeout handling for long-running operations

## Semantic Patterns

### Backend Patterns

**Command Execution Pattern:**
```go
func executeHauler(command string, args ...string) (string, error) {
    fullArgs := append([]string{command}, args...)
    cmd := exec.Command(\"hauler\", fullArgs...)
    cmd.Env = append(os.Environ(), \"HAULER_STORE=/data/store\")
    output, err := cmd.CombinedOutput()
    // Log output
    return string(output), err
}
```
- Centralized command execution
- Environment variable injection
- Combined stdout/stderr capture
- Logging for debugging

**Repository Pattern:**
```go
func loadRepositories() {
    repoFile := \"/data/config/repositories.json\"
    data, err := os.ReadFile(repoFile)
    if err != nil {
        return
    }
    json.Unmarshal(data, &repos)
}

func saveRepositories() error {
    repoFile := \"/data/config/repositories.json\"
    os.MkdirAll(filepath.Dir(repoFile), 0755)
    data, err := json.Marshal(repos)
    if err != nil {
        return err
    }
    return os.WriteFile(repoFile, data, 0644)
}
```
- Load/save pattern for persistence
- JSON serialization for configuration
- Directory creation before file write
- Silent failure on load (returns without error)

**HTTP Handler Pattern:**
```go
func handlerName(w http.ResponseWriter, r *http.Request) {
    var req RequestType
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, \"Invalid request\", http.StatusBadRequest)
        return
    }
    
    // Process request
    result, err := doSomething(req)
    if err != nil {
        respondError(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    respondJSON(w, Response{Success: true, Output: result})
}
```
- Decode request body to struct
- Early return on errors
- Consistent response format
- Appropriate HTTP status codes

**WebSocket Pattern:**
```go
func logsHandler(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()
    
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // Send updates
    }
}
```
- Upgrade HTTP to WebSocket
- Ticker for periodic updates
- Proper cleanup with defer
- Connection management

### Frontend Patterns

**API Call Pattern:**
```javascript
async function apiCall(endpoint, method = 'GET', body = null) {
    const options = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) options.body = JSON.stringify(body);
    const res = await fetch(`/api/${endpoint}`, options);
    return res.json();
}
```
- Centralized API communication
- Default parameters for common cases
- Consistent header setting
- JSON serialization/deserialization

**UI Update Pattern:**
```javascript
function updateUI() {
    const element = document.getElementById('elementId');
    element.innerHTML = data.map(item => `
        <div class=\"item\">
            ${item.name}
        </div>
    `).join('');
}
```
- Template literals for HTML generation
- Array map for list rendering
- Join to create single string
- Direct innerHTML assignment

**Form Handling Pattern:**
```javascript
async function handleFormSubmit() {
    const value = document.getElementById('inputId').value;
    if (!value) return alert('Value required');
    
    const outputEl = document.getElementById('outputId');
    outputEl.textContent = 'Processing...';
    
    const data = await apiCall('endpoint', 'POST', { value });
    outputEl.textContent = data.output || data.error;
}
```
- Input validation before submission
- User feedback during processing
- API call with error handling
- Result display in output element

**Modal Pattern:**
```javascript
function openModal() {
    const modal = document.getElementById('modalId');
    modal.classList.remove('hidden');
}

function closeModal() {
    const modal = document.getElementById('modalId');
    modal.classList.add('hidden');
}
```
- CSS class toggling for visibility
- Consistent show/hide pattern
- No framework dependencies

### Python MCP Server Patterns

**JSON-RPC Handler Pattern:**
```python
async def handle_message(self, message: dict) -> dict:
    method = message.get(\"method\")
    
    if method == \"initialize\":
        return {\"protocolVersion\": \"2024-11-05\", ...}
    elif method == \"tools/list\":
        return {\"tools\": self.tools}
    elif method == \"tools/call\":
        return await self.execute_tool(message[\"params\"])
    
    return {\"error\": \"Unknown method\"}
```
- Method dispatch based on message type
- Async execution for I/O operations
- Structured response format
- Error handling for unknown methods

**Tool Execution Pattern:**
```python
async def execute_tool(self, params: dict) -> dict:
    tool_name = params.get(\"name\")
    args = params.get(\"arguments\", {})
    
    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=30
        )
        return {\"content\": [{\"type\": \"text\", \"text\": output}]}
    except Exception as e:
        return {\"content\": [{\"type\": \"text\", \"text\": f\"Error: {str(e)}\"}], \"isError\": True}
```
- Parameter extraction from request
- Subprocess execution with timeout
- Structured response format
- Exception handling with error flag

## Common Code Idioms

### Go Idioms

**Defer for Cleanup:**
```go
file, err := os.Open(filename)
if err != nil {
    return err
}
defer file.Close()
```

**Early Return on Error:**
```go
if err != nil {
    return err
}
// Continue with success path
```

**Mutex Lock/Unlock:**
```go
mutex.Lock()
defer mutex.Unlock()
// Critical section
```

**HTTP Response Helper:**
```go
func respondJSON(w http.ResponseWriter, data interface{}) {
    w.Header().Set(\"Content-Type\", \"application/json\")
    json.NewEncoder(w).Encode(data)
}
```

### JavaScript Idioms

**Async/Await Error Handling:**
```javascript
try {
    const data = await apiCall('endpoint');
    // Process data
} catch (error) {
    console.error('Error:', error);
}
```

**Array Filtering and Mapping:**
```javascript
const filtered = items.filter(item => item.active);
const mapped = filtered.map(item => item.name);
```

**Template Literal HTML:**
```javascript
const html = `
    <div class=\"container\">
        <h1>${title}</h1>
        <p>${description}</p>
    </div>
`;
```

**Conditional Rendering:**
```javascript
element.innerHTML = items.length > 0 
    ? items.map(renderItem).join('') 
    : '<p>No items</p>';
```

### Python Idioms

**Context Managers:**
```python
with open(filename, 'r') as file:
    content = file.read()
```

**List Comprehensions:**
```python
filtered = [item for item in items if item.active]
```

**Dictionary Comprehensions:**
```python
mapping = {key: value for key, value in pairs}
```

**String Formatting:**
```python
message = f\"Error: {error_message}\"
```

## Architecture Patterns

### Three-Tier Architecture
- **Presentation Layer**: Browser-based UI (HTML/CSS/JavaScript)
- **Application Layer**: Go backend with REST API
- **Integration Layer**: Hauler CLI execution

### API Design
- RESTful endpoints with clear resource naming
- Consistent response format (success, output, error)
- HTTP status codes match operation results
- JSON for all request/response bodies

### State Management
- In-memory maps for runtime state (repositories, registries)
- File-based persistence for configuration
- WebSocket for real-time updates
- No database dependencies

### Error Handling
- Centralized error response functions
- User-friendly error messages
- Logging for debugging
- Graceful degradation

## Testing Patterns

### Manual Testing
- Browser-based UI testing
- API endpoint testing with curl/Postman
- Command execution verification
- File upload/download testing

### Integration Testing
- End-to-end workflow testing
- Multi-step operation verification
- Error scenario testing
- Performance testing under load

## Documentation Standards

### Code Comments
- Explain why, not what
- Document complex algorithms
- Note important assumptions
- Reference external resources when relevant

### API Documentation
- Endpoint purpose and usage
- Request/response examples
- Error conditions
- Authentication requirements

### README Files
- Project overview
- Installation instructions
- Usage examples
- Configuration options
