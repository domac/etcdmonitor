# Implementation Tasks: etcd Authentication Login UI

## Overview

This document breaks down the feature implementation into concrete, testable tasks organized by phase and priority.

## Phase 1: Backend Session Management (6-8 hours)

### T1.1: Add Auth Configuration Section [1 hour]
**File:** `internal/config/config.go`

**Tasks:**
1. Add `AuthConfig` struct with fields:
   - `Enable bool` (default: false)
   - `SessionTimeout int` (default: 3600)
   - `RememberDuration int` (default: 604800)
   - `SessionStorage string` (default: "memory")
   - `StoragePath string` (default: "data/sessions")

2. Add validation in `Load()` function:
   - Validate session_timeout > 0 and <= 604800
   - Validate remember_duration >= session_timeout and <= 2592000
   - Validate session_storage in ["memory", "file"]
   - Create storage_path if session_storage == "file"

3. Update config.yaml template with auth section

4. Add constants for defaults

**Acceptance Criteria:**
- ✅ Config struct parses YAML with all auth fields
- ✅ Defaults applied when fields omitted
- ✅ Invalid config values rejected with clear error
- ✅ Config.yaml template updated

**Test Cases:**
- [ ] Load config with auth.enable: true
- [ ] Load config without auth section (uses defaults)
- [ ] Reject invalid session_timeout (< 0 or > 604800)
- [ ] Reject invalid remember_duration

---

### T1.2: Create Session Package Structure [2 hours]
**Files:**
- `internal/auth/session.go` (new)
- `internal/auth/memory_store.go` (new)
- `internal/auth/authenticator.go` (new)

**T1.2.1: session.go - Core Types**

```go
package auth

import (
    "crypto/rand"
    "encoding/hex"
    "time"
)

// Session represents an active user session
type Session struct {
    Token       string
    Username    string
    CreatedAt   time.Time
    ExpiresAt   time.Time
    RememberMe  bool
}

// SessionStore interface for session persistence
type SessionStore interface {
    Create(username string, duration time.Duration, remember bool) (Session, error)
    Get(token string) (Session, error)
    Delete(token string) error
    Cleanup() error
}

// GenerateToken creates a cryptographically secure 64-char hex token
func GenerateToken() (string, error) {
    // Implementation
}

// IsExpired checks if session is still valid
func (s Session) IsExpired() bool {
    // Implementation
}
```

**T1.2.2: memory_store.go - In-Memory Implementation**

```go
package auth

type MemoryStore struct {
    // Fields
}

func NewMemoryStore() *MemoryStore {
    // Implementation
}

func (ms *MemoryStore) Create(...) (Session, error) {
    // Implementation
}

func (ms *MemoryStore) Get(...) (Session, error) {
    // Implementation
}

func (ms *MemoryStore) Delete(...) error {
    // Implementation
}

func (ms *MemoryStore) Cleanup() error {
    // Implementation
}

func (ms *MemoryStore) cleanupLoop() {
    // Background cleanup every 1 hour
}
```

**T1.2.3: authenticator.go - Credential Verification**

```go
package auth

type Authenticator struct {
    // Fields
}

func NewAuthenticator(cfg *config.Config) *Authenticator {
    // Implementation
}

func (a *Authenticator) VerifyCredentials(username, password string) error {
    // Implementation
}
```

**Acceptance Criteria:**
- ✅ Session struct contains all required fields
- ✅ SessionStore interface defined with 4 methods
- ✅ GenerateToken produces 64-char hex strings
- ✅ MemoryStore thread-safe with RWMutex
- ✅ MemoryStore auto-cleanup removes expired sessions hourly
- ✅ Authenticator verifies credentials against etcd /metrics endpoint
- ✅ No external dependencies (only Go stdlib)

**Test Cases:**
- [ ] GenerateToken returns 64-char hex string
- [ ] GenerateToken tokens are unique (min 100 generations)
- [ ] Session.IsExpired() returns true for expired sessions
- [ ] MemoryStore.Create() returns valid session
- [ ] MemoryStore.Get() returns session if valid
- [ ] MemoryStore.Get() returns error if expired
- [ ] MemoryStore.Delete() removes session
- [ ] MemoryStore.cleanup removes only expired sessions
- [ ] Authenticator.VerifyCredentials() succeeds with valid credentials
- [ ] Authenticator.VerifyCredentials() fails with invalid credentials

---

### T1.3: Implement Login API Handler [1.5 hours]
**File:** `internal/api/handler.go`

**Add Function:**
```go
func handleLogin(cfg *config.Config, store auth.SessionStore) http.HandlerFunc {
    // Implementation
}
```

**Responsibilities:**
1. Parse JSON request body (username, password, remember)
2. Validate input (non-empty, reasonable length)
3. Call authenticator.VerifyCredentials()
4. On success:
   - Create session via store.Create()
   - Set secure httpOnly cookie
   - Log username
   - Return 200 with session info
5. On failure:
   - Log failure (username but not password)
   - Return 401 with generic error message

**Request Validation:**
- Username: 1-256 characters
- Password: 1-512 characters
- Remember: boolean

**Response Format:**
```json
{
  "session_token": "...",
  "username": "...",
  "expires_at": "...",
  "remember_me": false
}
```

**Error Responses:**
- 400: Missing username/password
- 401: Invalid credentials
- 500: Session creation failed

**Acceptance Criteria:**
- ✅ Accepts valid login requests
- ✅ Rejects missing credentials with 400
- ✅ Rejects invalid credentials with 401 (generic message)
- ✅ Sets secure httpOnly cookie on success
- ✅ Returns session info JSON on success
- ✅ Logs all attempts (username only)

**Test Cases:**
- [ ] POST /api/auth/login with valid credentials
- [ ] POST /api/auth/login with empty username
- [ ] POST /api/auth/login with empty password
- [ ] POST /api/auth/login with invalid credentials
- [ ] Cookie has HttpOnly flag
- [ ] Cookie expires at correct time
- [ ] Response contains all required fields
- [ ] Credentials not logged or exposed

---

### T1.4: Implement Logout Handler [30 minutes]
**File:** `internal/api/handler.go`

**Add Function:**
```go
func handleLogout(store auth.SessionStore) http.HandlerFunc {
    // Implementation
}
```

**Responsibilities:**
1. Extract session cookie
2. Delete session from store (if exists)
3. Clear cookie (set MaxAge=-1)
4. Log logout event
5. Return 200 or 204

**Response:** Empty or `{"message": "Logged out"}`

**Acceptance Criteria:**
- ✅ Removes session from store
- ✅ Clears cookie (MaxAge=-1)
- ✅ Returns 200 or 204
- ✅ Works even if session doesn't exist

**Test Cases:**
- [ ] POST /api/auth/logout with valid session
- [ ] POST /api/auth/logout without session cookie
- [ ] Cookie cleared after logout
- [ ] Session deleted from store

---

### T1.5: Implement Status Handler [30 minutes]
**File:** `internal/api/handler.go`

**Add Function:**
```go
func handleAuthStatus(store auth.SessionStore) http.HandlerFunc {
    // Implementation
}
```

**Responsibilities:**
1. Check session cookie
2. If valid: return 200 with user info
3. If invalid/expired: return 401

**Response (authenticated):**
```json
{
  "authenticated": true,
  "username": "...",
  "expires_at": "...",
  "session_remaining_seconds": 3600
}
```

**Response (not authenticated):**
```json
{
  "authenticated": false
}
```

**Acceptance Criteria:**
- ✅ Returns 200 with user info if authenticated
- ✅ Returns 401 if not authenticated
- ✅ Calculates remaining seconds correctly

**Test Cases:**
- [ ] GET /api/auth/status with valid session → 200
- [ ] GET /api/auth/status without session → 401
- [ ] GET /api/auth/status with expired session → 401
- [ ] Remaining seconds calculated correctly

---

### T1.6: Create Auth Middleware [1 hour]
**File:** `internal/api/api.go`

**Add Function:**
```go
func authRequired(cfg *config.Config, store auth.SessionStore) func(http.Handler) http.Handler {
    // Implementation
}
```

**Behavior:**
1. If auth.enable == false → skip auth check
2. Check for session cookie
3. If missing → redirect to /login (302)
4. If present:
   - Validate via store.Get()
   - If valid → continue to handler
   - If invalid/expired → delete cookie + redirect to /login (302)

**Acceptance Criteria:**
- ✅ Skips auth check when auth.enable == false
- ✅ Redirects to /login when no cookie
- ✅ Allows request when session valid
- ✅ Redirects to /login when session invalid/expired
- ✅ Deletes cookie on expired session

**Test Cases:**
- [ ] Middleware skips check when auth disabled
- [ ] Middleware redirects to /login without cookie
- [ ] Middleware allows request with valid cookie
- [ ] Middleware redirects with invalid cookie
- [ ] Middleware redirects with expired cookie

---

### T1.7: Integrate Auth into Router [1 hour]
**File:** `internal/api/api.go`

**Changes:**
1. Initialize SessionStore (MemoryStore or FileStore)
2. Register new auth endpoints:
   - POST /api/auth/login (public)
   - POST /api/auth/logout (protected)
   - GET /api/auth/status (protected)
3. Wrap all existing endpoints with authRequired middleware:
   - GET /api/members
   - GET /api/current
   - GET /api/range
   - GET /api/status
   - GET /api/debug

**Acceptance Criteria:**
- ✅ SessionStore initialized correctly
- ✅ Auth endpoints registered
- ✅ All protected endpoints wrapped with middleware
- ✅ Public endpoints accessible without auth
- ✅ Router compiles and starts

**Test Cases:**
- [ ] POST /api/auth/login works
- [ ] Protected endpoints require auth
- [ ] Unprotected endpoints work without auth
- [ ] Server starts without errors

---

### T1.8: Add Session Cleanup to Main [30 minutes]
**File:** `cmd/etcdmonitor/main.go`

**Changes:**
1. Initialize SessionStore in main
2. Handle Cleanup() on graceful shutdown
3. Pass store to SetupRoutes()

**Acceptance Criteria:**
- ✅ SessionStore initialized
- ✅ Cleanup called on shutdown
- ✅ No goroutine leaks
- ✅ Graceful shutdown still works

**Test Cases:**
- [ ] Main starts with auth enabled
- [ ] Main starts with auth disabled
- [ ] Cleanup called on SIGINT/SIGTERM

---

## Phase 2: Frontend Login UI (4-5 hours)

### T2.1: Create login.html [1 hour]
**File:** `web/login.html` (new)

**Content:**
1. Responsive login form
2. Username input
3. Password input
4. Remember me checkbox
5. Submit button
6. Error message area
7. Loading indicator

**Requirements:**
- Semantic HTML5
- Accessibility (ARIA labels, keyboard navigation)
- Mobile-responsive
- Dark/light theme support (read from localStorage)

**Acceptance Criteria:**
- ✅ Form submits to POST /api/auth/login
- ✅ Inputs have proper types and labels
- ✅ Responsive on mobile/tablet/desktop
- ✅ Accessible (keyboard navigation, screen reader support)
- ✅ CSS references style.css

**Test Cases:**
- [ ] Form renders in browser
- [ ] Username/password inputs accept text
- [ ] Submit button clickable
- [ ] Responsive on mobile
- [ ] Keyboard navigation works

---

### T2.2: Create login.js [1.5 hours]
**File:** `web/login.js` (new)

**Responsibilities:**
1. Apply theme from localStorage before render
2. Handle form submission
3. Send credentials to /api/auth/login
4. Display error messages
5. Show loading state during submission
6. Redirect to dashboard on success
7. Handle network errors

**Code Structure:**
```javascript
// 1. Theme initialization
applyThemeBeforeRender();

// 2. Event listeners
document.getElementById('loginForm').addEventListener('submit', handleSubmit);

// 3. Helper functions
async function handleSubmit(e) { /* ... */ }
function showError(msg) { /* ... */ }
function showLoading(show) { /* ... */ }
```

**Acceptance Criteria:**
- ✅ Submits credentials to /api/auth/login
- ✅ Shows error message on 401
- ✅ Shows loading state during submission
- ✅ Redirects to / on success
- ✅ Handles network errors gracefully
- ✅ No external dependencies (vanilla JS)

**Test Cases:**
- [ ] Form submits on button click
- [ ] Loading state shown during submission
- [ ] Error displayed on invalid credentials
- [ ] Redirect on successful login
- [ ] Network error handled
- [ ] Theme applied from localStorage

---

### T2.3: Modify app.js for Authentication [1.5 hours]
**File:** `web/app.js`

**Changes:**
1. Add session check on page load
2. Create fetchWithAuth() wrapper for all API calls
3. Add logout button to header
4. Handle 401 responses (redirect to /login)

**New Functions:**
```javascript
// Check session before rendering dashboard
async function checkSession() {
    const response = await fetch('/api/auth/status');
    if (response.status === 401) {
        window.location.href = '/login';
        return false;
    }
    return true;
}

// Wrapper for authenticated API calls
async function fetchWithAuth(url, options = {}) {
    const response = await fetch(url, {
        ...options,
        credentials: 'include'
    });
    if (response.status === 401) {
        window.location.href = '/login';
    }
    return response;
}

// Logout handler
async function logout() {
    await fetch('/api/auth/logout', {method: 'POST', credentials: 'include'});
    window.location.href = '/login';
}
```

**Migration Strategy:**
- Replace all `fetch(...)` with `fetchWithAuth(...)`
- Add session check before dashboard initialization
- Add logout button to header UI

**Acceptance Criteria:**
- ✅ Session checked on page load
- ✅ Redirect to /login if not authenticated
- ✅ All API calls use fetchWithAuth
- ✅ 401 responses trigger redirect
- ✅ Logout button visible in header
- ✅ Logout works correctly

**Test Cases:**
- [ ] Page load without session redirects to /login
- [ ] Page load with valid session loads dashboard
- [ ] API calls include credentials
- [ ] 401 responses redirect to /login
- [ ] Logout button calls POST /api/auth/logout
- [ ] Logout redirects to /login

---

### T2.4: Update index.html for Logout [30 minutes]
**File:** `web/index.html`

**Changes:**
1. Add logout button to header (next to theme toggle)
2. Add session username display (optional)

**HTML Addition:**
```html
<button id="logoutBtn" class="btn btn-logout" title="Sign out">
  <i class="icon-sign-out"></i> Logout
</button>
```

**JavaScript Addition:**
```javascript
document.getElementById('logoutBtn').addEventListener('click', logout);
```

**Acceptance Criteria:**
- ✅ Logout button visible in header
- ✅ Button clickable
- ✅ Calls logout() function
- ✅ Responsive on mobile

**Test Cases:**
- [ ] Logout button visible
- [ ] Logout button clickable
- [ ] Logout redirects to /login

---

### T2.5: Add Login Page Styling [1 hour]
**File:** `web/style.css`

**Add Styles:**
1. .login-page container styling
2. .login-card form styling
3. .login-form input styling
4. .error-message styling
5. .loading-spinner animation
6. Responsive breakpoints (mobile, tablet, desktop)
7. Dark/light theme colors

**Acceptance Criteria:**
- ✅ Login page styled cleanly
- ✅ Responsive on all devices
- ✅ Supports dark/light themes
- ✅ Error messages visible
- ✅ Loading animation visible
- ✅ Accessible color contrast

**Test Cases:**
- [ ] Login page renders with styling
- [ ] Form centered on screen
- [ ] Responsive on mobile (320px)
- [ ] Responsive on tablet (768px)
- [ ] Responsive on desktop (1200px+)
- [ ] Dark theme works
- [ ] Light theme works

---

### T2.6: Update embed.go [30 minutes]
**File:** `embed.go`

**Changes:**
1. Add login.html to embedded files
2. Add login.js to embedded files
3. Update handler to serve login.html for GET /login

**Acceptance Criteria:**
- ✅ login.html embedded in binary
- ✅ login.js embedded in binary
- ✅ GET /login serves login.html
- ✅ login.html includes login.js script tag

**Test Cases:**
- [ ] Binary still compiles
- [ ] GET /login returns login.html
- [ ] login.js script loads

---

## Phase 3: Integration & Testing (3-4 hours)

### T3.1: Unit Tests - Session [1 hour]
**File:** `internal/auth/session_test.go` (new)

**Tests:**
```go
func TestGenerateToken(t *testing.T) { /* ... */ }
func TestGenerateTokenUniqueness(t *testing.T) { /* ... */ }
func TestSessionIsExpired(t *testing.T) { /* ... */ }
func TestMemoryStoreCreate(t *testing.T) { /* ... */ }
func TestMemoryStoreGet(t *testing.T) { /* ... */ }
func TestMemoryStoreDelete(t *testing.T) { /* ... */ }
func TestMemoryStoreExpiredCleanup(t *testing.T) { /* ... */ }
```

**Coverage Goal:** > 90%

---

### T3.2: Unit Tests - Authenticator [30 minutes]
**File:** `internal/auth/authenticator_test.go` (new)

**Tests:**
```go
func TestVerifyCredentialsValid(t *testing.T) { /* ... */ }
func TestVerifyCredentialsInvalid(t *testing.T) { /* ... */ }
func TestVerifyCredentialsUnreachable(t *testing.T) { /* ... */ }
```

---

### T3.3: Integration Tests - API [1 hour]
**File:** `internal/api/auth_test.go` (new)

**Tests:**
```go
func TestLoginSuccess(t *testing.T) { /* ... */ }
func TestLoginInvalidCredentials(t *testing.T) { /* ... */ }
func TestLoginMissingFields(t *testing.T) { /* ... */ }
func TestLogout(t *testing.T) { /* ... */ }
func TestAuthStatus(t *testing.T) { /* ... */ }
func TestProtectedEndpoint(t *testing.T) { /* ... */ }
func TestSessionExpiration(t *testing.T) { /* ... */ }
```

**Coverage Goal:** > 85%

---

### T3.4: Manual Testing Checklist [1.5 hours]

**Browser Testing:**
- [ ] Open dashboard without auth → loads normally (auth disabled)
- [ ] Open dashboard with auth enabled → redirects to /login
- [ ] Enter valid credentials → dashboard loads
- [ ] Enter invalid credentials → error message displayed
- [ ] Click logout → redirected to /login
- [ ] Wait for session timeout → redirected to /login
- [ ] Check "remember me" → session persists 7 days
- [ ] Open another browser tab → same session
- [ ] Close and reopen browser → session preserved (remember me)

**Security Testing:**
- [ ] Open DevTools → session token not visible in localStorage
- [ ] Cookie has HttpOnly flag (check with Set-Cookie header)
- [ ] Password never logged (check logs)
- [ ] HTTPS enforced when TLS enabled
- [ ] Session cookie not sent over HTTP

**Mobile Testing:**
- [ ] Login page responsive on iOS Safari
- [ ] Login page responsive on Android Chrome
- [ ] Touch keyboard doesn't obscure form
- [ ] Submit button easily clickable

---

### T3.5: End-to-End Testing [30 minutes]

**Scenario 1: Complete Login Flow**
1. Start server with `auth.enable: true`
2. Open browser → /
3. Redirected to /login
4. Enter credentials → Submit
5. Dashboard loads
6. Close browser
7. Open browser → /login again (session lost with browser restart)

**Scenario 2: Remember Me**
1. Login with "remember me" checked
2. Note expiry time
3. Wait 1 hour
4. Refresh page → still authenticated
5. Wait 7 days → session eventually expires

**Scenario 3: Logout**
1. Login successfully
2. Click logout button
3. Redirected to /login
4. Try accessing /api/members → 302 redirect

**Scenario 4: Session Expiration**
1. Login with 1-hour timeout
2. Wait 1 hour (or mock time)
3. Refresh page → redirected to /login

---

## Phase 4: Documentation & Deployment (2 hours)

### T4.1: Update README.md [45 minutes]

**Add Section: Authentication**
1. Feature overview
2. Configuration example
3. Enabling/disabling auth
4. Session management
5. Security notes
6. Troubleshooting

**Sections:**
```markdown
## Authentication (Optional)

### Overview
...

### Configuration
...

### Enabling Authentication
...

### Disable Authentication
...

### Security Considerations
...

### Troubleshooting
- Session expired message
- Cannot login
- Forgot password
```

---

### T4.2: Update config.yaml Template [15 minutes]

**Add Comments:**
```yaml
# Authentication (optional login UI)
auth:
  # Enable authentication login UI
  enable: false
  
  # Session timeout in seconds
  session_timeout: 3600
  
  # Remember me duration
  remember_duration: 604800
  
  # Session storage backend
  session_storage: memory
  
  # Session storage path
  storage_path: data/sessions
```

---

### T4.3: Test Deployment [30 minutes]

**Steps:**
1. Build binary with `./build.sh`
2. Create package with `./package.sh`
3. Extract on test server
4. Edit config.yaml: `auth.enable: true`
5. Run `install.sh`
6. Verify login works
7. Verify logout works
8. Verify systemd service works
9. Restart service, verify sessions respected

---

## Summary

**Total Estimated Effort:** 15-16 hours

**Phase Breakdown:**
- Phase 1 (Backend): 6-8 hours
- Phase 2 (Frontend): 4-5 hours
- Phase 3 (Testing): 3-4 hours
- Phase 4 (Documentation): 2 hours

**Critical Path:**
1. T1.2 (Session package) → T1.3-1.5 (Handlers) → T1.6-1.7 (Router)
2. T2.1 (login.html) + T2.2 (login.js) → T2.3 (app.js) → T2.5 (Styling)
3. T3.1-3.5 (Testing) - can run in parallel with Phase 2

**Prerequisites:**
- Go 1.21+ installed
- etcd cluster running with auth configured
- Basic familiarity with Go http package and middleware patterns

**Success Metrics:**
- ✅ All unit tests pass (> 85% coverage)
- ✅ All integration tests pass
- ✅ Manual testing checklist completed
- ✅ README updated with auth documentation
- ✅ Zero breaking changes to existing deployments
- ✅ Existing deployments unaffected (auth disabled by default)

