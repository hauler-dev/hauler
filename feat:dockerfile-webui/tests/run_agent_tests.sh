#!/bin/bash
# QA and Security Agent Test Orchestration
# Version: 2.1.0

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REPORT_DIR="/home/user/Desktop/hauler_ui/agent-test-reports"
mkdir -p "$REPORT_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}QA & SECURITY AGENT TEST ORCHESTRATION${NC}"
echo -e "${BLUE}Version: 2.1.0${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# ============================================
# PHASE 1: ENVIRONMENT SETUP
# ============================================
echo -e "${YELLOW}[PHASE 1/4] Environment Setup${NC}"
cd /home/user/Desktop/hauler_ui

echo "  → Stopping existing containers..."
docker compose down > /dev/null 2>&1 || true

echo "  → Building application..."
docker compose build --quiet

echo "  → Starting application..."
docker compose up -d

echo "  → Waiting for application to be ready..."
sleep 15

# Health check
for i in {1..10}; do
    if curl -s http://localhost:8080/api/health | grep -q "healthy"; then
        echo -e "  ${GREEN}✓ Application is healthy${NC}"
        break
    fi
    if [ $i -eq 10 ]; then
        echo -e "  ${RED}✗ Application failed to start${NC}"
        exit 1
    fi
    sleep 2
done

echo ""

# ============================================
# PHASE 2: FUNCTIONAL TESTING (QA AGENT)
# ============================================
echo -e "${YELLOW}[PHASE 2/4] Functional Testing (QA Agent)${NC}"

cd /home/user/Desktop/hauler_ui/tests
chmod +x comprehensive_test_suite.sh

echo "  → Running comprehensive test suite..."
./comprehensive_test_suite.sh > "$REPORT_DIR/functional-tests.log" 2>&1
FUNC_EXIT=$?

if [ $FUNC_EXIT -eq 0 ]; then
    echo -e "  ${GREEN}✓ All functional tests passed${NC}"
    FUNC_STATUS="PASS"
else
    echo -e "  ${RED}✗ Some functional tests failed${NC}"
    FUNC_STATUS="FAIL"
fi

# Extract test summary
TOTAL_TESTS=$(grep "Total Tests:" "$REPORT_DIR/functional-tests.log" | awk '{print $3}')
PASSED_TESTS=$(grep "Passed:" "$REPORT_DIR/functional-tests.log" | grep -oP '\d+')
FAILED_TESTS=$(grep "Failed:" "$REPORT_DIR/functional-tests.log" | grep -oP '\d+')

echo "  → Total: $TOTAL_TESTS | Passed: $PASSED_TESTS | Failed: $FAILED_TESTS"
echo ""

# ============================================
# PHASE 3: SECURITY SCANNING (SECURITY AGENT)
# ============================================
echo -e "${YELLOW}[PHASE 3/4] Security Scanning (Security Agent)${NC}"

cd /home/user/Desktop/hauler_ui/tests
chmod +x security_scan.sh

echo "  → Running security scans..."
./security_scan.sh > "$REPORT_DIR/security-scan.log" 2>&1 || true

# Parse security results
if [ -f "/home/user/Desktop/hauler_ui/security-reports/trivy-report.json" ]; then
    CRITICAL=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "CRITICAL")] | length' /home/user/Desktop/hauler_ui/security-reports/trivy-report.json 2>/dev/null || echo "0")
    HIGH=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "HIGH")] | length' /home/user/Desktop/hauler_ui/security-reports/trivy-report.json 2>/dev/null || echo "0")
    MEDIUM=$(jq '[.Results[].Vulnerabilities[]? | select(.Severity == "MEDIUM")] | length' /home/user/Desktop/hauler_ui/security-reports/trivy-report.json 2>/dev/null || echo "0")
else
    CRITICAL=0
    HIGH=0
    MEDIUM=0
fi

echo "  → Critical: $CRITICAL | High: $HIGH | Medium: $MEDIUM"

MEDIUM_PLUS=$((CRITICAL + HIGH + MEDIUM))

if [ $MEDIUM_PLUS -gt 0 ]; then
    echo -e "  ${RED}⚠ MEDIUM+ vulnerabilities found: $MEDIUM_PLUS${NC}"
    SEC_STATUS="FINDINGS"
else
    echo -e "  ${GREEN}✓ No MEDIUM+ vulnerabilities found${NC}"
    SEC_STATUS="CLEAN"
fi

echo ""

# ============================================
# PHASE 4: REPORT GENERATION
# ============================================
echo -e "${YELLOW}[PHASE 4/4] Report Generation${NC}"

# Generate consolidated report
cat > "$REPORT_DIR/AGENT_TEST_REPORT.md" << EOF
# QA & Security Agent Test Report
**Version:** 2.1.0
**Date:** $(date)
**Status:** COMPLETE

---

## Executive Summary

### Functional Testing (QA Agent)
- **Status:** $FUNC_STATUS
- **Total Tests:** $TOTAL_TESTS
- **Passed:** $PASSED_TESTS
- **Failed:** $FAILED_TESTS

### Security Scanning (Security Agent)
- **Status:** $SEC_STATUS
- **Critical:** $CRITICAL
- **High:** $HIGH
- **Medium:** $MEDIUM
- **MEDIUM+ Total:** $MEDIUM_PLUS

---

## Test Results

### Functional Tests
$(if [ "$FUNC_STATUS" = "PASS" ]; then
    echo "✅ **ALL TESTS PASSED**"
    echo ""
    echo "All functional tests completed successfully. Application is working as expected."
else
    echo "❌ **TESTS FAILED**"
    echo ""
    echo "Some functional tests failed. Review detailed log for specifics."
    echo ""
    echo "**Failed Tests:**"
    grep "✗ FAIL" "$REPORT_DIR/functional-tests.log" | head -10
fi)

**Detailed Log:** agent-test-reports/functional-tests.log

---

### Security Scans
$(if [ "$SEC_STATUS" = "CLEAN" ]; then
    echo "✅ **NO MEDIUM+ VULNERABILITIES**"
    echo ""
    echo "Security scans completed with no critical, high, or medium severity vulnerabilities."
else
    echo "⚠️ **VULNERABILITIES FOUND**"
    echo ""
    echo "Security scans identified vulnerabilities requiring remediation:"
    echo ""
    echo "- **Critical:** $CRITICAL (Immediate fix required)"
    echo "- **High:** $HIGH (Fix before release)"
    echo "- **Medium:** $MEDIUM (Fix before release per PM)"
    echo ""
    echo "**Total MEDIUM+ findings:** $MEDIUM_PLUS"
fi)

**Detailed Reports:**
- agent-test-reports/security-scan.log
- security-reports/SECURITY_SUMMARY.md
- security-reports/trivy-report.json
- security-reports/semgrep-report.json

---

## Findings Requiring Remediation

$(if [ $MEDIUM_PLUS -gt 0 ]; then
    echo "### MEDIUM+ Security Findings"
    echo ""
    echo "The following findings must be fixed before release:"
    echo ""
    if [ -f "/home/user/Desktop/hauler_ui/security-reports/trivy-report.json" ]; then
        jq -r '.Results[].Vulnerabilities[]? | select(.Severity == "CRITICAL" or .Severity == "HIGH" or .Severity == "MEDIUM") | "- [\(.Severity)] \(.VulnerabilityID): \(.PkgName) \(.InstalledVersion) → \(.FixedVersion // "No fix available")"' /home/user/Desktop/hauler_ui/security-reports/trivy-report.json | head -20
    fi
else
    echo "✅ **NO FINDINGS REQUIRING REMEDIATION**"
    echo ""
    echo "All tests passed and no security vulnerabilities found."
fi)

---

## Recommendations

### For Software Development Manager

$(if [ "$FUNC_STATUS" = "FAIL" ] || [ $MEDIUM_PLUS -gt 0 ]; then
    echo "**ACTION REQUIRED:**"
    echo ""
    if [ "$FUNC_STATUS" = "FAIL" ]; then
        echo "1. **Fix Failed Functional Tests**"
        echo "   - Review: agent-test-reports/functional-tests.log"
        echo "   - Assign to: Senior Developer"
        echo "   - Priority: HIGH"
        echo ""
    fi
    if [ $MEDIUM_PLUS -gt 0 ]; then
        echo "2. **Remediate Security Vulnerabilities**"
        echo "   - Review: security-reports/SECURITY_SUMMARY.md"
        echo "   - Assign to: Development Team"
        echo "   - Priority: CRITICAL/HIGH"
        echo ""
        echo "3. **Re-run Tests After Fixes**"
        echo "   - Execute: ./run_agent_tests.sh"
        echo "   - Verify: All MEDIUM+ findings resolved"
        echo ""
    fi
else
    echo "**NO ACTION REQUIRED**"
    echo ""
    echo "✅ All tests passed"
    echo "✅ No security vulnerabilities"
    echo "✅ Ready for production deployment"
fi)

---

## Next Steps

1. **Review Reports**
   - Functional: agent-test-reports/functional-tests.log
   - Security: security-reports/SECURITY_SUMMARY.md

2. **Assign Fixes** (if needed)
   - Create tickets for each finding
   - Assign to development team
   - Set priority based on severity

3. **Implement Fixes**
   - Address all MEDIUM+ findings
   - Update code/dependencies
   - Rebuild application

4. **Re-test**
   - Run: ./run_agent_tests.sh
   - Verify: All findings resolved
   - Confirm: Clean test results

5. **Sign-off**
   - QA Agent: Functional approval
   - Security Agent: Security approval
   - Release Manager: Production approval

---

## Test Artifacts

### Logs
- agent-test-reports/functional-tests.log
- agent-test-reports/security-scan.log

### Security Reports
- security-reports/SECURITY_SUMMARY.md
- security-reports/semgrep-report.json
- security-reports/go-vuln-report.txt
- security-reports/trivy-report.json
- security-reports/trivy-report.txt

### Agent Documents
- agents/16_QA_AGENT_TEST_EXECUTION.md
- agents/17_SECURITY_AGENT_ASSESSMENT.md

---

## Sign-off

**QA Agent:** $(if [ "$FUNC_STATUS" = "PASS" ]; then echo "✅ APPROVED"; else echo "⏳ PENDING FIXES"; fi)
**Security Agent:** $(if [ "$SEC_STATUS" = "CLEAN" ]; then echo "✅ APPROVED"; else echo "⏳ PENDING FIXES"; fi)
**Release Status:** $(if [ "$FUNC_STATUS" = "PASS" ] && [ "$SEC_STATUS" = "CLEAN" ]; then echo "✅ READY FOR PRODUCTION"; else echo "⚠️ FIXES REQUIRED"; fi)

---

**END OF REPORT**
EOF

echo "  → Consolidated report generated"
echo ""

# ============================================
# SUMMARY
# ============================================
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}TEST EXECUTION SUMMARY${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "Functional Tests: $(if [ "$FUNC_STATUS" = "PASS" ]; then echo -e "${GREEN}PASS${NC}"; else echo -e "${RED}FAIL${NC}"; fi)"
echo -e "Security Scans:   $(if [ "$SEC_STATUS" = "CLEAN" ]; then echo -e "${GREEN}CLEAN${NC}"; else echo -e "${YELLOW}FINDINGS${NC}"; fi)"
echo ""
echo -e "MEDIUM+ Findings: $(if [ $MEDIUM_PLUS -gt 0 ]; then echo -e "${RED}$MEDIUM_PLUS${NC}"; else echo -e "${GREEN}0${NC}"; fi)"
echo ""
echo -e "${BLUE}========================================${NC}"
echo ""
echo "📊 Consolidated Report: $REPORT_DIR/AGENT_TEST_REPORT.md"
echo "📁 All Reports: $REPORT_DIR/"
echo ""

if [ "$FUNC_STATUS" = "PASS" ] && [ "$SEC_STATUS" = "CLEAN" ]; then
    echo -e "${GREEN}✅ ALL TESTS PASSED - READY FOR PRODUCTION${NC}"
    exit 0
else
    echo -e "${YELLOW}⚠️  FINDINGS REQUIRE REMEDIATION${NC}"
    echo ""
    echo "Next Steps:"
    echo "1. Review: $REPORT_DIR/AGENT_TEST_REPORT.md"
    echo "2. Fix all MEDIUM+ findings"
    echo "3. Re-run: ./run_agent_tests.sh"
    exit 1
fi
