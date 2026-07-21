#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

REPORT_DIR="/scan/reports"
mkdir -p "$REPORT_DIR"

echo "========================================"
echo "SECURITY SCAN REPORT (CONTAINERIZED)"
echo "========================================"
echo "Timestamp: $(date)"
echo ""

# ============================================
# 1. CODE VULNERABILITY SCAN (Semgrep)
# ============================================
echo -e "${YELLOW}[1/3] Running Code Vulnerability Scan (Semgrep)...${NC}"

cd /scan/code

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
        jq -r '.results[] | select(.extra.severity == "ERROR" or .extra.severity == "WARNING") | "  - [\(.extra.severity)] \(.check_id): \(.path):\(.start.line)"' "$REPORT_DIR/semgrep-report.json" | head -20
    else
        echo -e "${GREEN}✓ No critical/high vulnerabilities in code${NC}"
    fi
else
    echo -e "${YELLOW}⚠ Semgrep scan failed${NC}"
fi

echo ""

# ============================================
# 2. DEPENDENCY VULNERABILITY SCAN (Go)
# ============================================
echo -e "${YELLOW}[2/3] Running Go Dependency Vulnerability Scan...${NC}"

cd /scan/code/backend

export PATH=$PATH:/go/bin
govulncheck ./... > "$REPORT_DIR/go-vuln-report.txt" 2>&1 || true

if grep -q "No vulnerabilities found" "$REPORT_DIR/go-vuln-report.txt"; then
    echo -e "${GREEN}✓ No vulnerabilities in Go dependencies${NC}"
else
    echo -e "${RED}⚠ Vulnerabilities found in Go dependencies!${NC}"
    head -30 "$REPORT_DIR/go-vuln-report.txt"
fi

echo ""

# ============================================
# 3. CONTAINER IMAGE SCAN (Trivy)
# ============================================
echo -e "${YELLOW}[3/3] Running Container Image Vulnerability Scan (Trivy)...${NC}"

trivy image --severity CRITICAL,HIGH,MEDIUM --format json --output "$REPORT_DIR/trivy-report.json" hauler_ui-hauler-ui:latest 2>/dev/null || true

if [ -f "$REPORT_DIR/trivy-report.json" ]; then
    CRITICAL=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity == "CRITICAL")] | length' "$REPORT_DIR/trivy-report.json" 2>/dev/null || echo "0")
    HIGH=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity == "HIGH")] | length' "$REPORT_DIR/trivy-report.json" 2>/dev/null || echo "0")
    MEDIUM=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity == "MEDIUM")] | length' "$REPORT_DIR/trivy-report.json" 2>/dev/null || echo "0")
    
    echo -e "  Critical: ${RED}$CRITICAL${NC}"
    echo -e "  High: ${YELLOW}$HIGH${NC}"
    echo -e "  Medium: ${GREEN}$MEDIUM${NC}"
    
    if [ "$CRITICAL" -gt 0 ] || [ "$HIGH" -gt 0 ]; then
        echo -e "${RED}⚠ CRITICAL/HIGH vulnerabilities found in container!${NC}"
        jq -r '.Results[]?.Vulnerabilities[]? | select(.Severity == "CRITICAL" or .Severity == "HIGH") | "  - [\(.Severity)] \(.VulnerabilityID): \(.PkgName) \(.InstalledVersion)"' "$REPORT_DIR/trivy-report.json" 2>/dev/null | head -20
    else
        echo -e "${GREEN}✓ No critical/high vulnerabilities in container${NC}"
    fi
    
    trivy image --severity CRITICAL,HIGH,MEDIUM --format table hauler_ui-hauler-ui:latest > "$REPORT_DIR/trivy-report.txt" 2>/dev/null || true
else
    echo -e "${YELLOW}⚠ Trivy scan failed${NC}"
fi

echo ""

# ============================================
# GENERATE SUMMARY REPORT
# ============================================
cat > "$REPORT_DIR/SECURITY_SUMMARY.md" << EOF
# Security Scan Report (Containerized)
**Generated:** $(date)

## Executive Summary

### Code Vulnerabilities (Semgrep)
- Critical: ${CRITICAL:-0}
- High: ${HIGH:-0}
- Medium: ${MEDIUM:-0}

### Go Dependencies (govulncheck)
$(if [ -f "$REPORT_DIR/go-vuln-report.txt" ]; then head -10 "$REPORT_DIR/go-vuln-report.txt"; else echo "Scan not completed"; fi)

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

echo -e "${GREEN}✓ Security scan complete!${NC}"
echo "Reports saved to: $REPORT_DIR"
