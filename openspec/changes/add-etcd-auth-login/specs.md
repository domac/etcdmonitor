# Technical Specifications: etcd Authentication Login UI

## 1. Configuration Specification

### config.yaml Structure
```yaml
auth:
  enable: false                    # Enable/disable login UI
  session_timeout: 3600           # Session duration in seconds (default: 1 hour)
  remember_duration: 604800       # Remember-me duration in seconds (default: 7 days)
  session_storage: memory         # "memory" or "file"
  storage_path: data/sessions     # Base directory for session files (only if storage: file)
```

### Configuration Defaults
```go
const (
    DefaultAuthEnable          = false
    DefaultSessionTimeout      = 3600              // 1 hour
    DefaultRememberDuration    = 604800            // 7 days
    DefaultSessionStorage      = "memory"
    DefaultSessionStoragePath  = "data/sessions"
)
```

### Validation Rules
1. `session_timeout` must be > 0 and < 604800 (7 days)
2. `remember_duration` must be >= `session_timeout` and <= 2592000 (30 days)
3. `session_storage` must be "memory" or "file"
4. `storage_path` must be writable if `session_storage` is "file"

## 2. API Specifications

### Authentication Endpoints

#### POST /api/auth/login
**Purpose:** Authenticate user credentials and create session

**Request:**
```json
{
  "username": "string",    // Required, non-empty
  "password": "string",    // Required, non-empty
  "remember": "boolean"    // Optional, default: false
}
```

**Constraints:**
- Username: max 256 characters
- Password: max 512 characters (for safety)
- username and password cannot be empty after trimming whitespace

**Response (200 OK):**
```json
{
  "session_token": "string",    // 64-character hex
  "username": "string",         // Authenticated username
  "expires_at": "RFC3339",      // ISO 8601 timestamp
  "remember_me": "boolean"
}
```

**Response (400 Bad Request):**
```json
{
  "error": "Missing username or password",
  "code": "INVALID_REQUEST"
}
```

**Response (401 Unauthorized):**
```json
{
  "error": "Invalid username or password",
  "code": "AUTH_FAILED"
}
```

**Response (500 Internal Server Error):**
```json
{
  "error": "Session creation failed",
  "code": "SESSION_ERROR"
}
```

**Side Effects:**
- Sets HTTP cookie `etcdmonitor_session` with session token
- Logs username (not password) to audit log
- Creates entry in SessionStore

**Rate Limiting:**
- No rate limiting in Phase 1 (future enhancement)

#### POST /api/auth/logout
**Purpose:** Invalidate current session and clear cookie

**Request:** Empty body

**Response (200 OK):**
```json
{
  "message": "Logged out successfully"
}
```

**Response (204 No Content):**
- Alternative response with no body

**Side Effects:**
- Clears `etcdmonitor_session` cookie (sets MaxAge=-1)
- Deletes session from SessionStore
- Logs logout event

#### GET /api/auth/status
**Purpose:** Check current session validity and user info

**Query Parameters:** None

**Response (200 OK - Authenticated):**
```json
{
  "authenticated": true,
  "username": "string",
  "expires_at": "RFC3339",
  "session_remaining_seconds": "integer"
}
```

**Response (401 Unauthorized):**
```json
{
  "authenticated": false
}
```

**Behavior:**
- No session cookie or invalid token → 401 response
- Valid session → 200 response with user details
- Expired session → 401 response (delete from store)

#### POST /api/auth/refresh (Future)
**Purpose:** Extend session duration if remember-me enabled

*Not implemented in Phase 1*

### Protected Endpoints

**All existing API endpoints become protected when `auth.enable: true`:**
- GET /api/members
- GET /api/current
- GET /api/range
- GET /api/status
- GET /api/debug

**Behavior:**
- Request has valid session cookie → proceed normally
- Request missing session cookie → redirect to /login (302 Found)
- Request has expired session → delete cookie + redirect to /login (302 Found)
- If `auth.enable: false` → all endpoints accessible without auth

### Public (Unauthenticated) Endpoints

**These remain public regardless of `auth.enable` setting:**
- GET /login (serve login.html)
- POST /api/auth/login (for logging in)

## 3. Session Management Specification

### Session Token Format
- **Length:** 64 characters (32 bytes hex-encoded)
- **Alphabet:** [0-9a-f]
- **Generation:** `crypto/rand.Read()` via `GenerateToken()`
- **Uniqueness:** Cryptographically guaranteed (< 1 in 2^256 collision probability)

### Session Data Structure
```go
type Session struct {
    Token       string        // 64-char hex token
    Username    string        // Authenticated user
    CreatedAt   time.Time     // UTC, when session created
    ExpiresAt   time.Time     // UTC, when session expires
    RememberMe  bool          // Extended expiration?
}
```

### Session Lifecycle

#### Creation
1. User submits credentials to POST /api/auth/login
2. Authenticator verifies via etcd /metrics endpoint
3. On success, SessionStore.Create() called:
   - Generates random token
   - Calculates expiry: now + session_timeout (or + remember_duration if remember=true)
   - Stores Session object
4. HTTP cookie set with token
5. Session is active

#### Validation
1. Incoming request includes `etcdmonitor_session` cookie
2. SessionStore.Get(token) called:
   - Returns session if token exists and not expired
   - Auto-deletes if expired
   - Returns error if not found or expired
3. Request proceeds if session valid
4. Request redirects to /login if invalid

#### Expiration
1. Automatic cleanup runs hourly (MemoryStore) or on startup (FileStore)
2. Expired sessions deleted from store
3. Next request with expired token → 401 + redirect to /login

#### Logout
1. POST /api/auth/logout called
2. SessionStore.Delete(token) removes session immediately
3. HTTP cookie set with MaxAge=-1 (delete cookie)
4. Subsequent requests have no session cookie → redirect to /login

### SessionStore Interface
```go
type SessionStore interface {
    // Create a new session
    Create(username string, duration time.Duration, remember bool) (Session, error)
    
    // Retrieve session by token
    Get(token string) (Session, error)
    
    // Delete session by token
    Delete(token string) error
    
    // Clean up expired sessions
    Cleanup() error
}
```

### MemoryStore Implementation

**Storage:** Go map in memory
```go
type MemoryStore struct {
    mu       sync.RWMutex
    sessions map[string]Session
    cleanup  *time.Ticker
}
```

**Characteristics:**
- Thread-safe with RWMutex
- O(1) operations (Create, Get, Delete)
- Auto-cleanup every 1 hour
- Sessions lost on process restart
- No persistent storage overhead

**Cleanup Logic:**
```go
func (ms *MemoryStore) cleanupLoop() {
    for range ms.cleanup.C {  // Every 1 hour
        ms.mu.Lock()
        for token, session := range ms.sessions {
            if session.IsExpired() {
                delete(ms.sessions, token)
                logger.Debugf("[Auth] Expired session cleaned: %s", token)
            }
        }
        ms.mu.Unlock()
    }
}
```

### FileStore Implementation (Future Phase)

**Storage:** JSON files on disk
```
data/sessions/
├── abc123def456.json      # Session file
├── xyz789uvw012.json
└── ...
```

**File Format:**
```json
{
  "token": "abc123def456...",
  "username": "root",
  "created_at": "2026-04-12T10:00:00Z",
  "expires_at": "2026-04-13T10:00:00Z",
  "remember_me": false
}
```

**Characteristics:**
- One file per session (token as filename)
- Sessions persist across restarts
- Cleanup on startup: delete all expired files
- O(1) file lookup by token
- Suitable for single-instance or NFS-mounted deployments

## 4. Cookie Specification

### Cookie Attributes

| Attribute | Value | Rationale |
|-----------|-------|-----------|
| Name | `etcdmonitor_session` | Unique namespace |
| Value | 64-char hex token | Session identifier |
| Path | `/` | Available to all paths |
| HttpOnly | true | Prevent JavaScript access (XSS protection) |
| Secure | true* | HTTPS only when TLS enabled |
| SameSite | Lax | CSRF protection, allow same-site requests |
| Expires | `<session_duration>` | Session expiry time |
| Domain | (omitted) | Use current domain only |

*Secure flag only set when `config.server.tls_enable: true`

### Cookie Handling

**Setting Cookie (after successful login):**
```go
http.SetCookie(w, &http.Cookie{
    Name:     "etcdmonitor_session",
    Value:    session.Token,
    Path:     "/",
    Expires:  session.ExpiresAt,
    HttpOnly: true,
    Secure:   cfg.Server.TLSEnable,
    SameSite: http.SameSiteLaxMode,
})
```

**Clearing Cookie (on logout):**
```go
http.SetCookie(w, &http.Cookie{
    Name:   "etcdmonitor_session",
    MaxAge: -1,  // Delete cookie
})
```

**Reading Cookie:**
```go
cookie, err := r.Cookie("etcdmonitor_session")
if err == http.ErrNoCookie {
    // No session cookie
}
```

## 5. Authentication Flow Specification

### Login Flow
```
User input: username, password, remember
    ↓
POST /api/auth/login
    ↓
1. Validate input (non-empty, reasonable length)
2. Extract request body
3. Authenticator.VerifyCredentials(username, password)
    ├─ HTTP GET to etcd /metrics endpoint with BasicAuth
    ├─ If 200 OK → credentials valid
    ├─ If 401 Unauthorized → invalid credentials
    ├─ If other error → authentication service unreachable
4. If credentials valid:
    ├─ SessionStore.Create(username, duration, remember)
    ├─ Set session cookie
    ├─ Log "User {username} logged in"
    └─ Return 200 with session info
5. If credentials invalid:
    ├─ Log "Authentication failed for {username}"
    ├─ Return 401 with error message
    └─ Do NOT set cookie
```

### Dashboard Load Flow
```
Browser: GET /
    ↓
1. Request arrives (no session cookie initially)
2. authRequired middleware checks:
    ├─ if !cfg.Auth.Enable → serve dashboard (no auth required)
    └─ if cfg.Auth.Enable:
        ├─ Check for session cookie
        ├─ If missing → redirect to /login (302)
        ├─ If present:
        │   ├─ SessionStore.Get(token) checks validity
        │   ├─ If valid → continue to dashboard handler
        │   └─ If invalid/expired → delete cookie + redirect to /login (302)
3. Browser receives redirect → GET /login
4. Server serves login.html
5. User submits credentials → login flow above
```

### Dashboard Access with Session
```
Browser: GET / (with session cookie)
    ↓
1. authRequired middleware validates session
2. Session found and not expired → continue
3. Serve dashboard HTML + JavaScript
4. Frontend JavaScript:
    ├─ GET /api/auth/status (verify session)
    ├─ GET /api/members (load members list)
    ├─ GET /api/current (load latest metrics)
    └─ GET /api/range (load time-series data)
5. All requests include session cookie automatically (browser behavior)
6. If any request gets 401 → frontend redirects to /login
```

### Session Expiration Flow
```
Time passes: session age > configured timeout
    ↓
1. Background cleanup removes expired session from store
2. Browser still has cookie (but token no longer valid in store)
3. Next API request (e.g., GET /api/members):
    ├─ Cookie included in request
    ├─ authRequired middleware calls SessionStore.Get(token)
    ├─ Returns error (token not found or expired)
    └─ Redirect to /login (302) + delete cookie
4. Frontend receives 302 → follows redirect → GET /login
5. Browser loads login page
```

## 6. Security Specifications

### Password Security
- **Transmission:** HTTPS only (enforced by Secure cookie flag when TLS enabled)
- **Storage:** Never stored; only used once to verify against etcd /metrics
- **Logging:** Username logged to audit, password NEVER logged
- **Memory:** Cleared immediately after verification

### Session Token Security
- **Generation:** `crypto/rand.Read()` provides cryptographically secure randomness
- **Size:** 256-bit token (64 hex characters) provides ~2^256 possible values
- **Storage:** Map (memory) or files (disk) with restricted permissions
- **Transmission:** HttpOnly cookie prevents JavaScript access
- **Validation:** Token must exist in store AND not be expired

### Input Validation
| Field | Rules | Max Length |
|-------|-------|-----------|
| username | Non-empty, trimmed | 256 chars |
| password | Non-empty | 512 chars |
| remember | Boolean | N/A |

### CSRF Protection
- **Mechanism:** SameSite=Lax cookie attribute
- **Behavior:** Cookie not sent in cross-origin requests
- **Verification:** Origin header validated for sensitive endpoints (future)

### XSS Protection
- **HttpOnly Cookie:** Session token inaccessible to JavaScript
- **CSP Headers:** Existing security headers prevent inline scripts
- **Input Sanitization:** No user input rendered to HTML without escaping

### Output Security
- **Response Bodies:** JSON only (no HTML injection vectors)
- **Error Messages:** Generic messages to prevent credential enumeration
  - ✗ "User 'root' not found"
  - ✓ "Invalid username or password"

## 7. Error Handling Specification

### HTTP Status Codes

| Status | Scenario | Response |
|--------|----------|----------|
| 200 | Login successful, session created | JSON with token |
| 204 | Logout successful | No content |
| 302 | Redirect to login | Location header |
| 400 | Bad request (malformed JSON) | Error JSON |
| 401 | Credentials invalid or session expired | Error JSON |
| 405 | Wrong HTTP method | Error message |
| 500 | Server error (session creation failed) | Error JSON |
| 503 | etcd endpoint unreachable | Error JSON |

### Error Response Format
```json
{
  "error": "Human-readable error message",
  "code": "ERROR_CODE"
}
```

### Error Codes
| Code | Meaning | HTTP Status |
|------|---------|-------------|
| INVALID_REQUEST | Missing/invalid fields in JSON | 400 |
| AUTH_FAILED | Credentials verification failed | 401 |
| SESSION_ERROR | SessionStore operation failed | 500 |
| ETCD_UNREACHABLE | Cannot reach etcd endpoint | 503 |
| INTERNAL_ERROR | Unexpected server error | 500 |

### Logging Specification

**Log Levels:**

| Event | Level | Message |
|-------|-------|---------|
| Login attempt (all) | Info | "[Auth] Login attempt for user {username}" |
| Login success | Info | "[Auth] User {username} logged in" |
| Login failure | Warn | "[Auth] Authentication failed for user {username}: {reason}" |
| Session created | Debug | "[Auth] Session created: {token_prefix}...{session_id}" |
| Session expired | Debug | "[Auth] Expired session cleaned: {token}" |
| Session deleted | Debug | "[Auth] Session deleted: {token}" |
| Logout | Info | "[Auth] User {username} logged out" |
| etcd unreachable | Warn | "[Auth] etcd endpoint unreachable: {error}" |

**Never Log:**
- Password (in any form)
- Full session token
- Request/response bodies containing credentials

## 8. Frontend Specification

### Login Page URL
- **Route:** GET /login
- **Served by:** http.FileServer with embedded web/login.html
- **Content-Type:** text/html; charset=utf-8

### Login Page Requirements
- Responsive design (mobile-friendly)
- Dark/light theme support (read from localStorage)
- Accessible (ARIA labels, keyboard navigation)
- Form validation (client-side before submit)
- Error message display area
- Loading indicator during submission
- Submit button with loading state

### JavaScript Requirements (app.js)

**Initialization:**
```javascript
// On page load
async function initApp() {
    const status = await fetch('/api/auth/status');
    if (!status.ok) {
        window.location.href = '/login';
        return;
    }
    // Proceed with dashboard initialization
}
```

**API Calls (wrap all fetch() calls):**
```javascript
async function fetchWithAuth(url, options = {}) {
    const response = await fetch(url, {
        ...options,
        credentials: 'include'  // Include cookies
    });
    if (response.status === 401) {
        window.location.href = '/login';
    }
    return response;
}
```

**Logout:**
```javascript
async function logout() {
    await fetch('/api/auth/logout', {method: 'POST', credentials: 'include'});
    window.location.href = '/login';
}
```

## 9. Go Stdlib and Imports

### Required Imports
```go
import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "sync"
    "time"
)
```

### No External Dependencies
- Session management uses only Go stdlib
- No JWT library needed (token is random hex, not JWT)
- No external crypto needed (stdlib crypto/rand sufficient)

## 10. Configuration File Template

```yaml
# Authentication (optional login UI)
auth:
  # Enable authentication login UI
  # false: dashboard accessible without password (default)
  # true: login page required, credentials verified against etcd
  enable: false
  
  # Session timeout in seconds (default: 3600 = 1 hour)
  # Must be between 60 and 604800 (7 days)
  session_timeout: 3600
  
  # "Remember me" duration in seconds (default: 604800 = 7 days)
  # Must be >= session_timeout and <= 2592000 (30 days)
  remember_duration: 604800
  
  # Session storage backend
  # "memory": sessions stored in RAM (lost on restart) - default
  # "file": sessions stored as JSON files on disk (persists across restarts)
  session_storage: memory
  
  # Directory for session storage (only used if session_storage: file)
  # Default: data/sessions
  storage_path: data/sessions
```

## 11. Version and Compatibility

### Target Go Version
- Go 1.21+ (same as main etcdmonitor requirement)

### Backward Compatibility
- Existing `config.yaml` without `auth:` section works unchanged
- All `auth.*` fields have sensible defaults
- Zero breaking changes to existing API endpoints (when auth.enable: false)

### Database/Storage Compatibility
- No schema changes needed
- No migrations required
- Can be added to existing etcdmonitor installations

