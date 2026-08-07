#!/bin/bash

set -e

echo "========================================"
echo "QA AGENT - DEPENDENCY VALIDATION TEST"
echo "========================================"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
warn() { echo -e "${YELLOW}⚠${NC} $1"; }

echo -e "\n${YELLOW}[1/6] Dockerfile Dependency Check${NC}"
grep -q "openssl" Dockerfile && pass "openssl included" || fail "openssl missing"
grep -q "ca-certificates" Dockerfile && pass "ca-certificates included" || fail "ca-certificates missing"
grep -q "curl" Dockerfile && pass "curl included" || fail "curl missing"
grep -q "bash" Dockerfile && pass "bash included" || fail "bash missing"

echo -e "\n${YELLOW}[2/6] Go Dependencies Check${NC}"
[ -f "backend/go.mod" ] && pass "go.mod exists" || fail "go.mod missing"
[ -f "backend/go.sum" ] && pass "go.sum exists" || fail "go.sum missing"
grep -q "gorilla/mux" backend/go.mod && pass "gorilla/mux declared" || fail "gorilla/mux missing"
grep -q "gorilla/websocket" backend/go.mod && pass "gorilla/websocket declared" || fail "gorilla/websocket missing"

echo -e "\n${YELLOW}[3/6] Docker Build Test${NC}"
echo "Building image (this may take a few minutes)..."
if sudo docker build -t hauler-ui-test . > /tmp/build.log 2>&1; then
    pass "Docker build successful"
else
    fail "Docker build failed - check /tmp/build.log"
fi

echo -e "\n${YELLOW}[4/6] Hauler Installation Verification${NC}"
if sudo docker run --rm hauler-ui-test hauler version > /dev/null 2>&1; then
    pass "Hauler installed correctly"
else
    fail "Hauler not installed or not working"
fi

echo -e "\n${YELLOW}[5/6] Runtime Dependencies Check${NC}"
sudo docker run --rm hauler-ui-test which curl > /dev/null && pass "curl available" || fail "curl missing"
sudo docker run --rm hauler-ui-test which bash > /dev/null && pass "bash available" || fail "bash missing"
sudo docker run --rm hauler-ui-test which openssl > /dev/null && pass "openssl available" || fail "openssl missing"

echo -e "\n${YELLOW}[6/6] Application Binary Check${NC}"
sudo docker run --rm hauler-ui-test test -f /app/hauler-ui && pass "hauler-ui binary exists" || fail "hauler-ui binary missing"
sudo docker run --rm hauler-ui-test test -f /app/static/index.html && pass "index.html exists" || fail "index.html missing"
sudo docker run --rm hauler-ui-test test -f /app/static/app.js && pass "app.js exists" || fail "app.js missing"

echo -e "\n${GREEN}========================================"
echo "ALL DEPENDENCY TESTS PASSED ✓"
echo "========================================${NC}"

sudo docker rmi hauler-ui-test > /dev/null 2>&1
