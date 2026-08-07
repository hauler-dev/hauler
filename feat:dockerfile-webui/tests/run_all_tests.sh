#!/bin/bash

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "========================================"
echo "HAULER UI - COMPLETE TEST SUITE"
echo "========================================"
echo ""

# Make scripts executable
chmod +x /home/user/Desktop/hauler_ui/tests/*.sh

# ============================================
# 1. COMPREHENSIVE FUNCTIONAL TESTS
# ============================================
echo -e "${BLUE}[PHASE 1/2] Running Comprehensive Functional Tests...${NC}"
echo ""

bash /home/user/Desktop/hauler_ui/tests/comprehensive_test_suite.sh
FUNCTIONAL_RESULT=$?

echo ""
echo ""

# ============================================
# 2. SECURITY VULNERABILITY SCAN
# ============================================
echo -e "${BLUE}[PHASE 2/2] Running Security Vulnerability Scan (Containerized)...${NC}"
echo ""

# Build security scanner image
echo "Building security scanner container..."
cd /home/user/Desktop/hauler_ui
sudo docker build -f Dockerfile.security -t hauler-security-scanner . --quiet 2>&1 | grep -v "^#" || true

# Run security scan in container
echo "Running security scans..."
sudo docker run --rm \
  -v /home/user/Desktop/hauler_ui:/scan/code:ro \
  -v /home/user/Desktop/hauler_ui/security-reports:/scan/reports \
  -v /var/run/docker.sock:/var/run/docker.sock \
  hauler-security-scanner

SECURITY_RESULT=$?

echo ""
echo ""

# ============================================
# FINAL SUMMARY
# ============================================
echo "========================================"
echo "FINAL TEST SUMMARY"
echo "========================================"

if [ $FUNCTIONAL_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ Functional Tests: PASSED${NC}"
else
    echo -e "${RED}✗ Functional Tests: FAILED${NC}"
fi

echo -e "${GREEN}✓ Security Scan: COMPLETED${NC}"
echo ""
echo "Detailed Reports:"
echo "  - Security Summary: /home/user/Desktop/hauler_ui/security-reports/SECURITY_SUMMARY.md"
echo "  - Code Vulnerabilities: /home/user/Desktop/hauler_ui/security-reports/semgrep-report.json"
echo "  - Container Vulnerabilities: /home/user/Desktop/hauler_ui/security-reports/trivy-report.txt"
echo ""

if [ $FUNCTIONAL_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ ALL TESTS COMPLETED SUCCESSFULLY${NC}"
    exit 0
else
    echo -e "${RED}✗ SOME TESTS FAILED - REVIEW LOGS ABOVE${NC}"
    exit 1
fi
