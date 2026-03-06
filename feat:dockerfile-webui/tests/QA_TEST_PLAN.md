# QA Test Plan - Hauler UI

## Overview
Comprehensive test suite covering all API endpoints, frontend functionality, and security vulnerabilities.

## Test Execution

### Quick Start
```bash
cd /home/user/Desktop/hauler_ui
bash tests/run_all_tests.sh
```

This single command runs:
1. **Comprehensive Functional Tests** (25+ test cases)
2. **Security Vulnerability Scans** (Code, Dependencies, Container)

## Test Coverage

### 1. Comprehensive Functional Tests (`comprehensive_test_suite.sh`)

#### Health & Connectivity (1 test)
- ✓ Service health check

#### Repository Management (4 tests)
- ✓ Add repository
- ✓ List repositories
- ✓ Fetch charts from repository
- ✓ Remove repository

#### Store Management (5 tests)
- ✓ Get store info
- ✓ Add image to store
- ✓ Verify image in store
- ✓ Add chart to store (without images)
- ✓ Verify chart in store

#### File Management (4 tests)
- ✓ Create test manifest file
- ✓ Upload manifest file
- ✓ List manifest files
- ✓ Download manifest file

#### Haul Management (3 tests)
- ✓ Save store to haul
- ✓ List haul files
- ✓ Download haul file

#### Server Management (4 tests)
- ✓ Check server status (stopped)
- ✓ Start registry server
- ✓ Check server status (running)
- ✓ Stop registry server

#### Command Execution (1 test)
- ✓ Execute custom Hauler command

#### Negative Tests (2 tests)
- ✓ Invalid repository name (404)
- ✓ Invalid file download (404)

**Total: 24 automated tests**

### 2. Security Vulnerability Scan (`security_scan.sh`)

#### Code Vulnerability Scan (Semgrep)
- Scans all source code for security issues
- Detects: SQL injection, XSS, command injection, etc.
- Severity levels: CRITICAL, HIGH, MEDIUM, LOW

#### Go Dependency Scan (govulncheck)
- Scans Go module dependencies
- Checks against Go vulnerability database
- Reports known CVEs in dependencies

#### Container Image Scan (Trivy)
- Scans Docker image for vulnerabilities
- Checks OS packages and application dependencies
- Reports: CRITICAL, HIGH, MEDIUM severity issues

## Test Reports

### Functional Test Output
- Real-time console output with color-coded results
- Pass/Fail summary at end
- Exit code 0 = all passed, 1 = failures

### Security Reports Location
```
/home/user/Desktop/hauler_ui/security-reports/
├── SECURITY_SUMMARY.md          # Executive summary
├── semgrep-report.json          # Code vulnerabilities (JSON)
├── go-vuln-report.txt           # Go dependency vulnerabilities
├── trivy-report.json            # Container vulnerabilities (JSON)
└── trivy-report.txt             # Container vulnerabilities (Human-readable)
```

## Severity Levels & Actions

### CRITICAL
- **Action**: Immediate fix required
- **Timeline**: Within 24 hours
- **Escalation**: Product Manager + SDM

### HIGH
- **Action**: Fix in next sprint
- **Timeline**: Within 1 week
- **Escalation**: SDM + Development Team

### MEDIUM
- **Action**: Schedule for upcoming release
- **Timeline**: Within 1 month
- **Review**: Weekly security review

### LOW
- **Action**: Backlog item
- **Timeline**: As time permits
- **Review**: Monthly security review

## CI/CD Integration

### Pre-Deployment Checklist
1. Run functional tests: `bash tests/comprehensive_test_suite.sh`
2. Run security scan: `bash tests/security_scan.sh`
3. Review security reports
4. Fix CRITICAL/HIGH issues
5. Re-run tests
6. Deploy

### Automated Testing
Add to CI/CD pipeline:
```yaml
test:
  script:
    - cd /path/to/hauler_ui
    - bash tests/run_all_tests.sh
  artifacts:
    paths:
      - security-reports/
```

## Manual Testing Checklist

### UI Functionality
- [ ] Dashboard displays store info
- [ ] Repository add/remove works
- [ ] Chart browser modal opens and lists charts
- [ ] Chart selection with version dropdown works
- [ ] Batch chart add to store works
- [ ] Store preview updates correctly
- [ ] Haul save triggers download
- [ ] Registry server start/stop works
- [ ] Logs display in real-time

### Edge Cases
- [ ] Large chart repositories (100+ charts)
- [ ] Network timeout handling
- [ ] Invalid chart versions
- [ ] Concurrent operations
- [ ] Browser compatibility (Chrome, Firefox, Safari)

## Known Limitations

1. **Network Dependency**: Tests require internet access for chart/image downloads
2. **Docker Requirement**: Container scan requires Docker daemon
3. **Timing Sensitivity**: Some tests use sleep() for async operations
4. **Resource Usage**: Full test suite may take 5-10 minutes

## Troubleshooting

### Tests Fail to Connect
```bash
# Check if service is running
curl http://localhost:8080/api/health

# Restart service
cd /home/user/Desktop/hauler_ui
sudo docker compose restart
```

### Security Scan Tools Missing
```bash
# Install Semgrep
pip3 install semgrep

# Install govulncheck
go install golang.org/x/vuln/cmd/govulncheck@latest

# Install Trivy
wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
echo "deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main" | sudo tee -a /etc/apt/sources.list.d/trivy.list
sudo apt-get update && sudo apt-get install trivy
```

## Maintenance

### Update Test Data
- Review test repositories quarterly
- Update chart versions in tests
- Verify external URLs still valid

### Security Database Updates
```bash
# Update Trivy vulnerability database
trivy image --download-db-only

# Update Go vulnerability database
govulncheck -db https://vuln.go.dev
```

## Contact

**QA Team Lead**: Review test results and reports
**Security Team**: Review security-reports/ directory
**Product Manager**: Escalate CRITICAL/HIGH findings
**Development Team**: Fix identified issues
