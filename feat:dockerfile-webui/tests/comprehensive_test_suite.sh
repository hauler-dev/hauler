#!/bin/bash
set -e

BASE_URL="http://localhost:8080"
PASS=0
FAIL=0
TOTAL=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_test() {
    TOTAL=$((TOTAL + 1))
    echo -e "\n${YELLOW}[TEST $TOTAL]${NC} $1"
}

pass() {
    PASS=$((PASS + 1))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

fail() {
    FAIL=$((FAIL + 1))
    echo -e "${RED}✗ FAIL${NC}: $1"
}

# ============================================
# HEALTH & CONNECTIVITY TESTS
# ============================================
log_test "Health Check"
RESPONSE=$(curl -s $BASE_URL/api/health)
if echo "$RESPONSE" | grep -q "healthy"; then
    pass "Service is healthy"
else
    fail "Service health check failed"
fi

# ============================================
# REPOSITORY MANAGEMENT TESTS
# ============================================
log_test "Add Repository"
RESPONSE=$(curl -s -X POST $BASE_URL/api/repos/add -H "Content-Type: application/json" -d '{"name":"test-repo","url":"https://charts.bitnami.com/bitnami"}')
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Repository added successfully"
else
    fail "Failed to add repository"
fi

log_test "List Repositories"
RESPONSE=$(curl -s $BASE_URL/api/repos/list)
if echo "$RESPONSE" | grep -q "test-repo"; then
    pass "Repository listed successfully"
else
    fail "Repository not found in list"
fi

log_test "Fetch Charts from Repository"
RESPONSE=$(curl -s $BASE_URL/api/repos/charts/test-repo)
if echo "$RESPONSE" | grep -q "charts"; then
    CHART_COUNT=$(echo "$RESPONSE" | jq -r '.charts | length')
    pass "Fetched $CHART_COUNT charts from repository"
else
    fail "Failed to fetch charts from repository"
fi

log_test "Remove Repository"
RESPONSE=$(curl -s -X DELETE $BASE_URL/api/repos/remove/test-repo)
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Repository removed successfully"
else
    fail "Failed to remove repository"
fi

# ============================================
# STORE MANAGEMENT TESTS
# ============================================
log_test "Get Store Info"
RESPONSE=$(curl -s $BASE_URL/api/store/info)
if echo "$RESPONSE" | grep -q "REFERENCE"; then
    pass "Store info retrieved successfully"
else
    fail "Failed to get store info"
fi

log_test "Add Image to Store"
RESPONSE=$(curl -s -X POST $BASE_URL/api/store/add-content -H "Content-Type: application/json" -d '{"type":"image","name":"alpine:latest"}')
if echo "$RESPONSE" | grep -q "success"; then
    pass "Image added to store"
else
    fail "Failed to add image to store"
fi

sleep 3

log_test "Verify Image in Store"
RESPONSE=$(curl -s $BASE_URL/api/store/info)
if echo "$RESPONSE" | grep -q "alpine"; then
    pass "Image verified in store"
else
    fail "Image not found in store"
fi

log_test "Add Chart to Store (without images)"
RESPONSE=$(curl -s -X POST $BASE_URL/api/store/add-content -H "Content-Type: application/json" -d '{"type":"chart","name":"nginx","version":"18.2.4","repository":"https://charts.bitnami.com/bitnami","addImages":false,"addDependencies":false}')
if echo "$RESPONSE" | grep -q "successfully added chart"; then
    pass "Chart added to store without images"
else
    fail "Failed to add chart to store"
fi

sleep 2

log_test "Verify Chart in Store"
RESPONSE=$(curl -s $BASE_URL/api/store/info)
if echo "$RESPONSE" | grep -q "nginx"; then
    pass "Chart verified in store"
else
    fail "Chart not found in store"
fi

# ============================================
# FILE MANAGEMENT TESTS
# ============================================
log_test "Create Test Manifest File"
cat > ~/test-manifest.yaml << 'EOF'
apiVersion: v1
kind: Images
spec:
  images:
    - name: busybox:latest
EOF

log_test "Upload Manifest File"
RESPONSE=$(curl -s -X POST $BASE_URL/api/files/upload -F "file=@$HOME/test-manifest.yaml" -F "type=manifest")
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Manifest file uploaded successfully"
else
    fail "Failed to upload manifest file"
fi

log_test "List Manifest Files"
RESPONSE=$(curl -s "$BASE_URL/api/files/list?type=manifest")
if echo "$RESPONSE" | grep -q "test-manifest.yaml"; then
    pass "Manifest file listed successfully"
else
    fail "Manifest file not found in list"
fi

log_test "Download Manifest File"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/files/download/test-manifest.yaml?type=manifest")
if [ "$HTTP_CODE" = "200" ]; then
    pass "Manifest file downloaded successfully"
else
    fail "Failed to download manifest file (HTTP $HTTP_CODE)"
fi

# ============================================
# HAUL MANAGEMENT TESTS
# ============================================
log_test "Save Store to Haul"
RESPONSE=$(curl -s -X POST $BASE_URL/api/store/save -H "Content-Type: application/json" -d '{"filename":"test-haul.tar.zst"}')
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Store saved to haul successfully"
else
    fail "Failed to save store to haul"
fi

sleep 2

log_test "List Haul Files"
RESPONSE=$(curl -s "$BASE_URL/api/files/list?type=haul")
if echo "$RESPONSE" | grep -q "test-haul.tar.zst"; then
    pass "Haul file listed successfully"
else
    fail "Haul file not found in list"
fi

log_test "Download Haul File"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/files/download/test-haul.tar.zst?type=haul")
if [ "$HTTP_CODE" = "200" ]; then
    pass "Haul file downloaded successfully"
else
    fail "Failed to download haul file (HTTP $HTTP_CODE)"
fi

# ============================================
# SERVER MANAGEMENT TESTS
# ============================================
log_test "Check Server Status (should be stopped)"
RESPONSE=$(curl -s $BASE_URL/api/serve/status)
if echo "$RESPONSE" | grep -q "running.*false"; then
    pass "Server status correctly shows stopped"
else
    fail "Server status check failed"
fi

log_test "Start Registry Server"
RESPONSE=$(curl -s -X POST $BASE_URL/api/serve/start -H "Content-Type: application/json" -d '{"port":"5555"}')
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Registry server started successfully"
else
    fail "Failed to start registry server"
fi

sleep 2

log_test "Check Server Status (should be running)"
RESPONSE=$(curl -s $BASE_URL/api/serve/status)
if echo "$RESPONSE" | grep -q "running.*true"; then
    pass "Server status correctly shows running"
else
    fail "Server status check failed"
fi

log_test "Stop Registry Server"
RESPONSE=$(curl -s -X POST $BASE_URL/api/serve/stop)
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Registry server stopped successfully"
else
    fail "Failed to stop registry server"
fi

# ============================================
# COMMAND EXECUTION TESTS
# ============================================
log_test "Execute Custom Hauler Command"
RESPONSE=$(curl -s -X POST $BASE_URL/api/command -H "Content-Type: application/json" -d '{"command":"version"}')
if echo "$RESPONSE" | grep -q "success.*true"; then
    pass "Custom command executed successfully"
else
    fail "Failed to execute custom command"
fi

# ============================================
# NEGATIVE TESTS
# ============================================
log_test "Invalid Repository Name"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" $BASE_URL/api/repos/charts/nonexistent-repo)
if [ "$HTTP_CODE" = "404" ]; then
    pass "Correctly returned 404 for nonexistent repository"
else
    fail "Did not handle nonexistent repository correctly"
fi

log_test "Invalid File Download"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/files/download/nonexistent.tar.zst?type=haul")
if [ "$HTTP_CODE" = "404" ]; then
    pass "Correctly returned 404 for nonexistent file"
else
    fail "Did not handle nonexistent file correctly"
fi

# ============================================
# SUMMARY
# ============================================
echo ""
echo "========================================"
echo "TEST SUMMARY"
echo "========================================"
echo -e "Total Tests: $TOTAL"
echo -e "${GREEN}Passed: $PASS${NC}"
echo -e "${RED}Failed: $FAIL${NC}"
echo "========================================"

if [ $FAIL -eq 0 ]; then
    echo -e "${GREEN}✓ ALL TESTS PASSED${NC}"
    exit 0
else
    echo -e "${RED}✗ SOME TESTS FAILED${NC}"
    exit 1
fi
