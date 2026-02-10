# QA Test Plan - New Features v2.1.0
**Date:** 2024
**Features:** System Reset & Private Registry Push
**Status:** READY FOR TESTING

---

## Test Environment Setup

### Prerequisites
```bash
cd /home/user/Desktop/hauler_ui
docker compose up -d
```

### Test Data Required
- Harbor registry instance (or Docker registry)
- Test credentials
- Sample haul files
- Sample manifests

---

## TEST SUITE 1: System Reset Feature

### Test Case 1.1: Reset Button Visibility
**Objective:** Verify reset button is accessible

**Steps:**
1. Navigate to Settings tab
2. Scroll to "Danger Zone" section
3. Verify "Reset Hauler System" button is visible

**Expected Result:**
- ✅ Button visible with red styling
- ✅ Warning icon displayed
- ✅ Clear description text present

**Status:** ⬜ PENDING

---

### Test Case 1.2: Reset Confirmation Dialogs
**Objective:** Verify double confirmation prevents accidental resets

**Steps:**
1. Click "Reset Hauler System" button
2. Observe first confirmation dialog
3. Click "Cancel"
4. Verify no reset occurs
5. Click button again
6. Click "OK" on first dialog
7. Click "Cancel" on second dialog
8. Verify no reset occurs

**Expected Result:**
- ✅ First warning dialog appears
- ✅ Second confirmation dialog appears
- ✅ Cancel at any point prevents reset
- ✅ Both confirmations required

**Status:** ⬜ PENDING

---

### Test Case 1.3: Successful Reset Execution
**Objective:** Verify reset clears store successfully

**Steps:**
1. Add content to store (images/charts)
2. Verify store has content via "Store Info"
3. Navigate to Settings
4. Click "Reset Hauler System"
5. Confirm both dialogs
6. Wait for completion
7. Check store info

**Expected Result:**
- ✅ Reset completes successfully
- ✅ Store is empty
- ✅ Success message displayed
- ✅ No errors in output

**Status:** ⬜ PENDING

---

### Test Case 1.4: Files Preserved After Reset
**Objective:** Verify uploaded files remain after reset

**Steps:**
1. Upload haul file
2. Upload manifest file
3. Verify files in respective tabs
4. Perform system reset
5. Check Hauls tab
6. Check Manifests tab

**Expected Result:**
- ✅ Haul files still present
- ✅ Manifest files still present
- ✅ Only store content cleared

**Status:** ⬜ PENDING

---

### Test Case 1.5: Reset Error Handling
**Objective:** Verify error handling if reset fails

**Steps:**
1. Stop hauler service (simulate failure)
2. Attempt reset
3. Observe error message

**Expected Result:**
- ✅ Error message displayed
- ✅ No system crash
- ✅ User informed of failure

**Status:** ⬜ PENDING

---

## TEST SUITE 2: Private Registry Push Feature

### Test Case 2.1: Registry Configuration UI
**Objective:** Verify registry configuration form works

**Steps:**
1. Navigate to "Push to Registry" tab
2. Verify form fields present
3. Enter registry details:
   - Name: test-harbor
   - URL: harbor.test.com
   - Username: admin
   - Password: Harbor12345
4. Click "Add Registry"
5. Verify registry appears in list

**Expected Result:**
- ✅ All form fields present
- ✅ Registry saved successfully
- ✅ Registry appears in configured list
- ✅ Password masked in display

**Status:** ⬜ PENDING

---

### Test Case 2.2: Registry CRUD Operations
**Objective:** Verify full lifecycle management

**Steps:**
1. Add registry "test-reg-1"
2. Add registry "test-reg-2"
3. Verify both appear in list
4. Delete "test-reg-1"
5. Verify only "test-reg-2" remains
6. Refresh page
7. Verify "test-reg-2" still present

**Expected Result:**
- ✅ Multiple registries supported
- ✅ Delete works correctly
- ✅ Configuration persists across refreshes

**Status:** ⬜ PENDING

---

### Test Case 2.3: Password Security
**Objective:** Verify passwords are secured

**Steps:**
1. Add registry with password "SecretPass123"
2. Check registry list display
3. Check browser developer tools
4. Check /data/config/registries.json file permissions
5. Check application logs

**Expected Result:**
- ✅ Password shows as "***" in UI
- ✅ Password not visible in network responses
- ✅ File has 0600 permissions
- ✅ Password not in logs

**Status:** ⬜ PENDING

---

### Test Case 2.4: Connection Testing
**Objective:** Verify registry connection test

**Steps:**
1. Configure valid Harbor registry
2. Click "Test" button
3. Observe output
4. Configure invalid registry
5. Click "Test" button
6. Observe error

**Expected Result:**
- ✅ Valid registry shows success
- ✅ Invalid registry shows error
- ✅ Clear feedback to user

**Status:** ⬜ PENDING

---

### Test Case 2.5: Push to Harbor Registry
**Objective:** Verify content push to Harbor

**Steps:**
1. Add content to store (nginx:latest)
2. Configure Harbor registry
3. Select registry from dropdown
4. Click "Push All Content to Registry"
5. Confirm dialog
6. Wait for completion
7. Verify in Harbor UI

**Expected Result:**
- ✅ Push completes successfully
- ✅ Content visible in Harbor
- ✅ Progress feedback shown
- ✅ Success message displayed

**Status:** ⬜ PENDING

---

### Test Case 2.6: Push Authentication Failure
**Objective:** Verify error handling for auth failures

**Steps:**
1. Configure registry with wrong password
2. Attempt to push content
3. Observe error message

**Expected Result:**
- ✅ Clear authentication error shown
- ✅ No system crash
- ✅ User can retry with correct credentials

**Status:** ⬜ PENDING

---

### Test Case 2.7: Insecure Registry Option
**Objective:** Verify insecure flag works

**Steps:**
1. Configure registry with self-signed cert
2. Enable "Allow insecure connection"
3. Test connection
4. Push content

**Expected Result:**
- ✅ Connection succeeds with insecure flag
- ✅ Push completes successfully
- ✅ Warning about insecure connection

**Status:** ⬜ PENDING

---

### Test Case 2.8: Push to Docker Registry
**Objective:** Verify compatibility with Docker registry

**Steps:**
1. Configure Docker registry
2. Push content
3. Verify in registry

**Expected Result:**
- ✅ Works with Docker registry
- ✅ Content pushed successfully

**Status:** ⬜ PENDING

---

## TEST SUITE 3: Integration Tests

### Test Case 3.1: Complete Workflow
**Objective:** Test full airgap workflow

**Steps:**
1. Add Helm repository
2. Browse and select charts
3. Add charts to store
4. Add Docker images
5. Save to haul file
6. Reset system
7. Load haul file
8. Configure Harbor registry
9. Push to Harbor
10. Verify in Harbor

**Expected Result:**
- ✅ Complete workflow succeeds
- ✅ All features work together
- ✅ Content available in Harbor

**Status:** ⬜ PENDING

---

### Test Case 3.2: Multiple Registry Push
**Objective:** Verify pushing to multiple registries

**Steps:**
1. Configure Harbor registry
2. Configure Docker registry
3. Add content to store
4. Push to Harbor
5. Push to Docker registry
6. Verify in both

**Expected Result:**
- ✅ Content in both registries
- ✅ No conflicts
- ✅ Both pushes succeed

**Status:** ⬜ PENDING

---

## TEST SUITE 4: Security Tests

### Test Case 4.1: Credential Storage Security
**Objective:** Verify credentials stored securely

**Steps:**
1. Add registry with credentials
2. Check file permissions
3. Attempt to read as non-root user
4. Check for encryption

**Expected Result:**
- ✅ File permissions 0600
- ✅ Only root can read
- ✅ Credentials not in plaintext (if encrypted)

**Status:** ⬜ PENDING

---

### Test Case 4.2: XSS Prevention
**Objective:** Verify no XSS vulnerabilities

**Steps:**
1. Enter `<script>alert('xss')</script>` as registry name
2. Save registry
3. Verify no script execution

**Expected Result:**
- ✅ Script not executed
- ✅ Input sanitized
- ✅ Display escaped

**Status:** ⬜ PENDING

---

## TEST SUITE 5: Performance Tests

### Test Case 5.1: Reset Performance
**Objective:** Verify reset completes quickly

**Steps:**
1. Add large amount of content
2. Perform reset
3. Measure time

**Expected Result:**
- ✅ Reset completes in < 10 seconds
- ✅ UI remains responsive

**Status:** ⬜ PENDING

---

### Test Case 5.2: Large Push Performance
**Objective:** Verify large content push

**Steps:**
1. Add 100+ images to store
2. Push to registry
3. Monitor progress

**Expected Result:**
- ✅ Push completes successfully
- ✅ Progress feedback shown
- ✅ No timeouts

**Status:** ⬜ PENDING

---

## TEST SUITE 6: Browser Compatibility

### Test Case 6.1: Chrome/Edge
**Status:** ⬜ PENDING

### Test Case 6.2: Firefox
**Status:** ⬜ PENDING

### Test Case 6.3: Safari
**Status:** ⬜ PENDING

---

## Defect Tracking

| ID | Severity | Description | Status |
|----|----------|-------------|--------|
| - | - | - | - |

---

## Test Summary

**Total Test Cases:** 25
**Passed:** 0
**Failed:** 0
**Blocked:** 0
**Pending:** 25

---

## Sign-Off

**QA Engineer:** ⬜ PENDING
**Date:** ___________

**Ready for Production:** ⬜ YES / ⬜ NO

---

**TESTING BEGINS NOW**
