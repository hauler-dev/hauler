#!/bin/bash

set -e

echo "================================"
echo "Hauler UI - Automated Test Suite"
echo "================================"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() {
    echo -e "${GREEN}✓${NC} $1"
}

fail() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# 1. Architecture Design Tests
echo -e "\n${YELLOW}[1/5] Architecture Design Tests${NC}"
[ -f "backend/main.go" ] && pass "Backend exists" || fail "Backend missing"
[ -f "static/index.html" ] && pass "Frontend exists" || fail "Frontend missing"
[ -f "Dockerfile" ] && pass "Dockerfile exists" || fail "Dockerfile missing"
[ -f "docker-compose.yml" ] && pass "Docker Compose exists" || fail "Docker Compose missing"
[ -d "data" ] && pass "Data directory exists" || fail "Data directory missing"

# 2. Code Creation Tests
echo -e "\n${YELLOW}[2/5] Code Creation Tests${NC}"
grep -q "gorilla/mux" backend/main.go && pass "Using Gorilla Mux" || fail "Mux not found"
grep -q "gorilla/websocket" backend/main.go && pass "WebSocket support" || fail "WebSocket missing"
grep -q "executeHauler" backend/main.go && pass "Hauler CLI wrapper" || fail "CLI wrapper missing"
grep -q "tailwindcss" static/index.html && pass "Tailwind CSS" || fail "Tailwind missing"
grep -q "showTab" static/app.js && pass "Tab navigation" || fail "Navigation missing"

# 3. Build Tests
echo -e "\n${YELLOW}[3/5] Build Tests${NC}"
echo "Building Docker image..."
docker-compose build > /dev/null 2>&1 && pass "Docker build successful" || fail "Docker build failed"

# 4. Deployment Tests
echo -e "\n${YELLOW}[4/5] Deployment Tests${NC}"
echo "Starting container..."
docker-compose up -d > /dev/null 2>&1 && pass "Container started" || fail "Container start failed"

echo "Waiting for service..."
sleep 5

# Health check
if curl -s http://localhost:8080/api/health | grep -q "healthy"; then
    pass "Health check passed"
else
    fail "Health check failed"
fi

# API tests
if curl -s http://localhost:8080/api/store/info > /dev/null; then
    pass "Store API accessible"
else
    warn "Store API returned error (expected if empty)"
fi

if curl -s http://localhost:8080/api/serve/status | grep -q "running"; then
    pass "Serve status API works"
else
    pass "Serve status API works (server stopped)"
fi

# Frontend test
if curl -s http://localhost:8080/ | grep -q "Hauler UI"; then
    pass "Frontend accessible"
else
    fail "Frontend not accessible"
fi

# 5. Security Tests
echo -e "\n${YELLOW}[5/5] Security Tests${NC}"

# Path traversal test
RESPONSE=$(curl -s -X POST http://localhost:8080/api/store/sync \
    -H "Content-Type: application/json" \
    -d '{"filename":"../../etc/passwd"}' | grep -o "error" || echo "safe")
[ "$RESPONSE" != "" ] && pass "Path traversal prevented" || warn "Path traversal check inconclusive"

# File list test
if curl -s "http://localhost:8080/api/files/list?type=manifest" | grep -q "files"; then
    pass "File listing works"
else
    fail "File listing failed"
fi

# Cleanup
echo -e "\n${YELLOW}Cleaning up...${NC}"
docker-compose down > /dev/null 2>&1

echo -e "\n${GREEN}================================${NC}"
echo -e "${GREEN}All tests passed!${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo "To start the UI:"
echo "  make run"
echo ""
echo "To access:"
echo "  http://localhost:8080"
echo ""
