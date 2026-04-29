#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

REPORT_DIR="/home/user/Desktop/hauler_ui/security-reports"
mkdir -p "$REPORT_DIR"

echo "========================================"
echo "SECURITY SCAN REPORT"
echo "========================================"
echo "Timestamp: $(date)"
echo ""

# ============================================
# 1. CODE VULNERABILITY SCAN (Semgrep)
# ============================================
echo -e "${YELLOW}[1/3] Running Code Vulnerability Scan (Semgrep)...${NC}"

if ! command -v semgrep &> /dev/null; then
    echo "Installing Semgrep..."
    pip3 install semgrep --quiet 2>/dev/null || python3 -m pip install semgrep --quiet
fi

cd /home/user/Desktop/hauler_ui

semgrep --config=auto --json --output="$REPORT_DIR/semgrep-report.json" . 2>/dev/null || true

if [ -f "$REPORT_DIR/semgrep-report.json" ]; then
    CRITICAL=$(jq '[.results[] | select(.extra.severity == "ERROR")] | length' "$REPORT_DIR/semgrep-report.json")
    HIGH=$(jq '[.results[] | select(.extra.severity == "WARNING")] | length' "$REPORT_DIR/semgrep-report.json")
    MEDIUM=$(jq '[.results[] | select(.extra.severity == "INFO")] | length' "$REPORT_DIR/semgrep-report.json")
    
    echo -e "  Critical: ${RED}$CRITICAL${NC}"
    echo -e "  High: ${YELLOW}$HIGH${NC}"
    echo -e "  Medium: ${GREEN}$MEDIUM${NC}"
    
    if [ "$CRITICAL" -gt 0 ] || [ "$HIGH" -gt 0 ]; then
        echo -e "${RED}⚠ CRITICAL/HIGH vulnerabilities found in code!${NC}"
        jq -r '.results[] | select(.extra.severity == "ERROR" or .extra.severity == "WARNING") | "  - [\(.extra.severity)] \(.check_id): \(.path):\(.start.line)"' "$REPORT_DIR/semgrep-report.json"
    else
        echo -e "${GREEN}✓ No critical/high vulnerabilities in code${NC}"
    fi
else
    echo -e "${YELLOW}⚠ Semgrep scan skipped or failed${NC}"
fi

echo ""

# ============================================
# 2. DEPENDENCY VULNERABILITY SCAN (Go)
# ============================================
echo -e "${YELLOW}[2/3] Running Go Dependency Vulnerability Scan...${NC}"

cd /home/user/Desktop/hauler_ui/backend

if command -v govulncheck &> /dev/null; then
    govulncheck ./... > "$REPORT_DIR/go-vuln-report.txt" 2>&1 || true
    
    if grep -q "No vulnerabilities found" "$REPORT_DIR/go-vuln-report.txt"; then
        echo -e "${GREEN}✓ No vulnerabilities in Go dependencies${NC}"
    else
        echo -e "${RED}⚠ Vulnerabilities found in Go dependencies!${NC}"
        cat "$REPORT_DIR/go-vuln-report.txt"
    fi
else
    echo "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
    export PATH=$PATH:$(go env GOPATH)/bin
    govulncheck ./... > "$REPORT_DIR/go-vuln-report.txt" 2>&1 || true
fi

echo ""

# ============================================
# 3. CONTAINER IMAGE SCAN (Trivy)
# ============================================
echo -e "${YELLOW}[3/3] Running Container Image Vulnerability Scan (Trivy)...${NC}"

if ! command -v trivy &> /dev/null; then
    echo "Installing Trivy..."
    wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
    echo "deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main" | sudo tee -a /etc/apt/sources.list.d/trivy.list
    sudo apt-get update -qq
    sudo apt-get install trivy -y -qq
fi

cd /home/user/Desktop/hauler_ui

echo "Building Docker image for scanning..."
sudo docker compose build --quiet 2>&1 | grep -v "^#" || true

echo "Scanning Docker image..."
sudo trivy image --severity CRITICAL,HIGH,MEDIUM --format json --output "$REPORT_DIR/trivy-report.json" hauler_ui-hauler-ui:latest 2>/dev/null || true

if [ -f "$REPORT_DIR/trivy-report.json" ]; then
    CRITICAL=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "CRITICAL")] | length' "$REPORT_DIR/trivy-report.json")
    HIGH=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "HIGH")] | length' "$REPORT_DIR/trivy-report.json")
    MEDIUM=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "MEDIUM")] | length' "$REPORT_DIR/trivy-report.json")
    
    echo -e "  Critical: ${RED}$CRITICAL${NC}"
    echo -e "  High: ${YELLOW}$HIGH${NC}"
    echo -e "  Medium: ${GREEN}$MEDIUM${NC}"
    
    if [ "$CRITICAL" -gt 0 ] || [ "$HIGH" -gt 0 ]; then
        echo -e "${RED}⚠ CRITICAL/HIGH vulnerabilities found in container!${NC}"
        jq -r '.Results[].Vulnerabilities[]? | select(.Severity == "CRITICAL" or .Severity == "HIGH") | "  - [\(.Severity)] \(.VulnerabilityID): \(.PkgName) \(.InstalledVersion)"' "$REPORT_DIR/trivy-report.json" | head -20
    else
        echo -e "${GREEN}✓ No critical/high vulnerabilities in container${NC}"
    fi
    
    # Generate human-readable report
    sudo trivy image --severity CRITICAL,HIGH,MEDIUM --format table hauler_ui-hauler-ui:latest > "$REPORT_DIR/trivy-report.txt" 2>/dev/null || true
else
    echo -e "${YELLOW}⚠ Trivy scan skipped or failed${NC}"
fi

echo ""

# ============================================
# GENERATE SUMMARY REPORT
# ============================================
echo "========================================"
echo "SECURITY SCAN SUMMARY"
echo "========================================"

cat > "$REPORT_DIR/SECURITY_SUMMARY.md" << EOF
# Security Scan Report
**Generated:** $(date)

## Executive Summary

### Code Vulnerabilities (Semgrep)
- Critical: $CRITICAL
- High: $HIGH
- Medium: $MEDIUM

### Go Dependencies (govulncheck)
$(if [ -f "$REPORT_DIR/go-vuln-report.txt" ]; then cat "$REPORT_DIR/go-vuln-report.txt" | head -10; else echo "Scan not completed"; fi)

### Container Image (Trivy)
- Critical: ${CRITICAL:-0}
- High: ${HIGH:-0}
- Medium: ${MEDIUM:-0}

## Detailed Reports
- Code Scan: security-reports/semgrep-report.json
- Go Dependencies: security-reports/go-vuln-report.txt
- Container Scan: security-reports/trivy-report.json
- Container Scan (Human): security-reports/trivy-report.txt

## Recommendations
EOF

# Add recommendations based on findings
if [ "${CRITICAL:-0}" -gt 0 ] || [ "${HIGH:-0}" -gt 0 ]; then
    cat >> "$REPORT_DIR/SECURITY_SUMMARY.md" << EOF

⚠️ **IMMEDIATE ACTION REQUIRED**
- Critical/High vulnerabilities detected
- Review detailed reports in security-reports/ directory
- Update dependencies and rebuild container
- Re-run security scan after fixes
EOF
else
    cat >> "$REPORT_DIR/SECURITY_SUMMARY.md" << EOF

✅ **NO CRITICAL ISSUES FOUND**
- No critical or high severity vulnerabilities detected
- Continue monitoring for new vulnerabilities
- Schedule regular security scans
EOF
fi

echo ""
echo -e "${GREEN}✓ Security scan complete!${NC}"
echo "Reports saved to: $REPORT_DIR"
echo ""
echo "View summary: cat $REPORT_DIR/SECURITY_SUMMARY.md"
