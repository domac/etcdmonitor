# Design: etcd Authentication Login UI

## Overview

This document provides the detailed technical design for implementing an authentication login UI in etcdmonitor. The solution enables runtime credential management while maintaining backward compatibility with existing deployments.

## Architecture

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                        Browser                               │
│  ┌─────────────┐  Session Cookie  ┌─────────────────┐       │
│  │ Login Page  │◄────────────────►│ Dashboard HTML  │       │
│  └──────┬──────┘                  └────────┬────────┘       │
│         │                                   │                │
│         │ POST /api/auth/login             │ GET /api/*     │
│         │ (username, password)             │                │
│         └──────────────┬────────────────────┘                │
│                        │                                     │
├────────────────────────┼─────────────────────────────────────┤
│                 etcdmonitor Server                            │
│                        │                                     │
│        ┌───────────────▼──────────────────┐                 │
│        │   HTTP Request Handler            │                 │
│        │  (Add/Validate Session Cookie)    │                 │
│        └────────────────┬─────────────────┘                 │
│                        │                                     │
│        ┌───────────────▼──────────────────┐                 │
│        │  Router / Middleware              │                 │
│        │  - authRequired (for /api/*)      │                 │
│        │  - securityHeaders                │                 │
│        │  - corsValidator                  │                 │
│        └───┬─────────┬─────────┬──────────┘                 │
│            │         │         │                             │
│    ┌───────▼─┐  ┌────▼────┐  ┌▼──────────────┐             │
│    │Auth API │  │Public   │  │Protected APIs │             │
│    │/login   │  │Routes   │  │/members, etc  │             │
│    │/logout  │  │/login   │  │               │             │
│    │/status  │  │/        │  │               │             │
│    └─────────┘  └─────────┘  └┬──────────────┘             │
│                                │                             │
│        ┌───────────────────────▼──────────┐                │
│        │    Session Manager                │                │
│        │  ┌─────────────────────────────┐  │                │
│        │  │ SessionStore Interface:     │  │                │
│        │  │ - Create(user, remember)    │  │                │
│        │  │ - Get(token)                │  │                │
│        │  │ - Delete(token)             │  │                │
│        │  │ - Cleanup()                 │  │                │
│        │  └─────────────────────────────┘  │                │
│        └───┬──────────────────────┬────────┘                │
│            │                      │                         │
│     ┌──────▼────┐        ┌────────▼───────┐               │
│     │MemoryStore│        │  FileStore     │               │
│     │(in-memory │        │(data/sessions) │               │
│     │ map)      │        │                │               │
│     └───────────┘        └────────────────┘               │
│                                │                           │
│        ┌───────────────────────▼──────────────┐            │
│        │  Credential Verification             │            │
│        │  (test login via etcd endpoint)      │            │
│        └─────────────────────────────────────┘            │
│                        │                                   │
│                        ▼                                   │
│        ┌─────────────────────────────┐                    │
│        │   etcd Cluster              │                    │
│        │   /metrics endpoint         │                    │
│        │   (with BasicAuth)          │                    │
│        └─────────────────────────────┘                    │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

### Data Flow: Login

```
1. User opens dashboard
   ↓
2. Browser: GET / (no session cookie)
   ↓
3. Server: Check session → not found → serve login.html
   ↓
4. Browser displays login form
   ↓
5. User enters credentials → POST /api/auth/login
   ↓
6. Server: Authenticate via etcd /metrics (BasicAuth)
   ├─ Create Session object (token, username, expiry)
   ├─ Store in SessionStore (MemoryStore or FileStore)
   ├─ Set httpOnly cookie: etcdmonitor_session=<token>
   └─ Return {session_token, expires_at}
   ↓
7. Browser: Redirect to dashboard (cookie auto-included)
   ↓
8. Server: GET /api/members
   ├─ Extract session cookie
   ├─ Validate token → found and not expired
   └─ Return members data
   ↓
9. Browser: Display dashboard with collected metrics
```

## File Structure

### New Files

```
etcdmonitor/
├── internal/auth/
│   ├── session.go          # Session struct, SessionStore interface
│   ├── memory_store.go     # In-memory session storage
│   ├── file_store.go       # File-based persistent storage
│   └── authenticator.go    # Verify credentials against etcd
├── web/
│   ├── login.html          # Login form UI
│   └── login.js            # Login form handling + session management
└── openspec/
    └── changes/
        └── add-etcd-auth-login/
            ├── design.md        # This file
            ├── proposal.md      # Feature proposal
            ├── specs.md         # Detailed technical specs
            └── tasks.md         # Implementation tasks
```

### Modified Files

```
├── cmd/etcdmonitor/main.go          # Start session cleanup goroutine
├── internal/config/config.go        # Add Auth config section
├── internal/api/api.go              # Add auth endpoints, middleware
├── internal/api/handler.go          # Implement auth handlers
├── web/index.html                   # Add logout button + session status
├── web/app.js                       # Add session check + auth headers
├── web/style.css                    # Add login page styling
├── config.yaml                      # Add auth configuration template
├── go.mod                           # No new dependencies needed
└── embed.go                         # Include login.html in embed
```

## Data Structures

### Session
```go
type Session struct {
    Token       string        // 32-byte hex token
    Username    string        // etcd username
    CreatedAt   time.Time     // session creation time
    ExpiresAt   time.Time     // session expiration
    RememberMe  bool          // extend expiration on refresh?
}
```

### Config Addition
```go
type AuthConfig struct {
    Enable           bool          // Default: false
    SessionTimeout   int           // seconds, default: 3600 (1 hour)
    RememberDuration int           // seconds, default: 604800 (7 days)
    SessionStorage   string        // "memory" or "file", default: "memory"
    StoragePath      string        // default: "data/sessions"
}
```

### API Payloads

#### POST /api/auth/login
**Request:**
```json
{
  "username": "root",
  "password": "changeme",
  "remember": false
}
```

**Response (200 OK):**
```json
{
  "session_token": "a1b2c3d4e5f6...",
  "username": "root",
  "expires_at": "2026-04-12T14:30:00Z",
  "remember_me": false
}
```

**Response (401 Unauthorized):**
```json
{
  "error": "Invalid username or password"
}
```

#### POST /api/auth/logout
**Request:** Empty body

**Response (200 OK):**
```json
{
  "message": "Logged out successfully"
}
```

#### GET /api/auth/status
**Response (200 OK - Authenticated):**
```json
{
  "authenticated": true,
  "username": "root",
  "expires_at": "2026-04-12T14:30:00Z"
}
```

**Response (401 Unauthorized - Not Authenticated):**
```json
{
  "authenticated": false
}
```

## Frontend Implementation Details

### Login Page Layout
```html
<!DOCTYPE html>
<html>
<head>
  <title>etcd Monitor - Login</title>
  <link rel="stylesheet" href="style.css">
</head>
<body class="login-page">
  <div class="login-container">
    <div class="login-card">
      <h1>etcd Monitor</h1>
      <p class="subtitle">Authentication Required</p>
      
      <form id="loginForm" class="login-form">
        <div class="form-group">
          <label for="username">Username</label>
          <input 
            type="text" 
            id="username" 
            name="username"
            placeholder="Enter username"
            autocomplete="username"
            required
            autofocus
          >
        </div>
        
        <div class="form-group">
          <label for="password">Password</label>
          <input 
            type="password" 
            id="password" 
            name="password"
            placeholder="Enter password"
            autocomplete="current-password"
            required
          >
        </div>
        
        <div class="form-group checkbox">
          <input 
            type="checkbox" 
            id="remember" 
            name="remember"
          >
          <label for="remember">Remember me for 7 days</label>
        </div>
        
        <button type="submit" class="btn btn-primary" id="submitBtn">
          Sign In
        </button>
        
        <div id="errorMessage" class="error-message" style="display: none;"></div>
        <div id="loadingIndicator" class="loading-spinner" style="display: none;"></div>
      </form>
      
      <p class="footer-text">
        etcd Monitor v<span id="version">0.2.0</span>
      </p>
    </div>
  </div>
  
  <script src="login.js"></script>
</body>
</html>
```

### login.js Responsibilities
```javascript
// 1. Apply saved theme before page renders (prevent theme flash)
applyThemeBeforeRender();

// 2. Form submission handler
document.getElementById('loginForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('username').value;
  const password = document.getElementById('password').value;
  const remember = document.getElementById('remember').checked;
  
  showLoading(true);
  
  try {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({username, password, remember})
    });
    
    if (response.ok) {
      const data = await response.json();
      // Session cookie set by server
      window.location.href = '/';  // Redirect to dashboard
    } else {
      const error = await response.json();
      showError(error.error || 'Login failed');
    }
  } catch (err) {
    showError('Network error: ' + err.message);
  } finally {
    showLoading(false);
  }
});

// 3. Helper functions
function showLoading(show) { /* ... */ }
function showError(message) { /* ... */ }
function applyThemeBeforeRender() {
  const theme = localStorage.getItem('etcdmonitor_theme') || 'dark';
  document.documentElement.setAttribute('data-theme', theme);
}
```

### app.js Modifications

**On page load:**
```javascript
// Check session before rendering dashboard
async function initializeApp() {
  try {
    const response = await fetch('/api/auth/status');
    if (response.status === 401) {
      window.location.href = '/login';
      return;
    }
    const status = await response.json();
    if (!status.authenticated) {
      window.location.href = '/login';
      return;
    }
    // Session valid, continue loading dashboard
    initializeDashboard();
  } catch (err) {
    console.error('Session check failed:', err);
    window.location.href = '/login';
  }
}
```

**Add auth headers to all API calls:**
```javascript
// Wrapper for all fetch() calls
async function fetchWithAuth(url, options = {}) {
  const response = await fetch(url, {
    ...options,
    credentials: 'include'  // Include cookies
  });
  
  if (response.status === 401) {
    window.location.href = '/login';  // Redirect if session expired
    return;
  }
  
  return response;
}

// Use in all places instead of fetch():
// OLD: const members = await fetch('/api/members')
// NEW: const members = await fetchWithAuth('/api/members')
```

**Add logout button to header:**
```javascript
// In dashboard header
<button id="logoutBtn" class="btn-logout" title="Sign out">
  <i class="icon-sign-out"></i> Logout
</button>

document.getElementById('logoutBtn').addEventListener('click', async () => {
  await fetch('/api/auth/logout', {method: 'POST'});
  window.location.href = '/login';
});
```

## Backend Implementation Details

### internal/auth/session.go
```go
package auth

import (
  "crypto/rand"
  "encoding/hex"
  "time"
)

type Session struct {
  Token       string
  Username    string
  CreatedAt   time.Time
  ExpiresAt   time.Time
  RememberMe  bool
}

type SessionStore interface {
  Create(username string, duration time.Duration, remember bool) (Session, error)
  Get(token string) (Session, error)
  Delete(token string) error
  Cleanup() error
}

// GenerateToken creates a cryptographically secure session token
func GenerateToken() (string, error) {
  b := make([]byte, 32)
  _, err := rand.Read(b)
  if err != nil {
    return "", err
  }
  return hex.EncodeToString(b), nil
}

// IsExpired checks if session is still valid
func (s Session) IsExpired() bool {
  return time.Now().After(s.ExpiresAt)
}
```

### internal/auth/memory_store.go
```go
package auth

import (
  "fmt"
  "sync"
  "time"
)

type MemoryStore struct {
  mu       sync.RWMutex
  sessions map[string]Session
  cleanup  *time.Ticker
}

func NewMemoryStore() *MemoryStore {
  ms := &MemoryStore{
    sessions: make(map[string]Session),
    cleanup:  time.NewTicker(1 * time.Hour),
  }
  go ms.cleanupLoop()
  return ms
}

func (ms *MemoryStore) Create(username string, duration time.Duration, remember bool) (Session, error) {
  token, err := GenerateToken()
  if err != nil {
    return Session{}, fmt.Errorf("token generation failed: %w", err)
  }
  
  session := Session{
    Token:       token,
    Username:    username,
    CreatedAt:   time.Now(),
    ExpiresAt:   time.Now().Add(duration),
    RememberMe:  remember,
  }
  
  ms.mu.Lock()
  ms.sessions[token] = session
  ms.mu.Unlock()
  
  return session, nil
}

func (ms *MemoryStore) Get(token string) (Session, error) {
  ms.mu.RLock()
  session, ok := ms.sessions[token]
  ms.mu.RUnlock()
  
  if !ok {
    return Session{}, fmt.Errorf("session not found")
  }
  if session.IsExpired() {
    ms.mu.Lock()
    delete(ms.sessions, token)
    ms.mu.Unlock()
    return Session{}, fmt.Errorf("session expired")
  }
  
  return session, nil
}

func (ms *MemoryStore) Delete(token string) error {
  ms.mu.Lock()
  delete(ms.sessions, token)
  ms.mu.Unlock()
  return nil
}

func (ms *MemoryStore) cleanupLoop() {
  for range ms.cleanup.C {
    ms.mu.Lock()
    for token, session := range ms.sessions {
      if session.IsExpired() {
        delete(ms.sessions, token)
      }
    }
    ms.mu.Unlock()
  }
}

func (ms *MemoryStore) Cleanup() error {
  ms.cleanup.Stop()
  return nil
}
```

### internal/auth/authenticator.go
```go
package auth

import (
  "fmt"
  "io"
  "net/http"
  "time"

  "etcdmonitor/internal/config"
)

type Authenticator struct {
  cfg    *config.Config
  client *http.Client
}

func NewAuthenticator(cfg *config.Config) *Authenticator {
  return &Authenticator{
    cfg: cfg,
    client: &http.Client{Timeout: 5 * time.Second},
  }
}

// VerifyCredentials attempts to authenticate with etcd /metrics endpoint
func (a *Authenticator) VerifyCredentials(username, password string) error {
  metricsURL := a.cfg.Etcd.Endpoint + a.cfg.Etcd.MetricsPath
  
  req, err := http.NewRequest("GET", metricsURL, nil)
  if err != nil {
    return fmt.Errorf("request creation failed: %w", err)
  }
  
  req.SetBasicAuth(username, password)
  
  resp, err := a.client.Do(req)
  if err != nil {
    return fmt.Errorf("authentication request failed: %w", err)
  }
  defer resp.Body.Close()
  
  // Discard response body to prevent resource leak
  io.ReadAll(resp.Body)
  
  if resp.StatusCode == http.StatusOK {
    return nil
  }
  if resp.StatusCode == http.StatusUnauthorized {
    return fmt.Errorf("invalid credentials")
  }
  
  return fmt.Errorf("authentication failed with status %d", resp.StatusCode)
}
```

### Router & Middleware Changes (api.go)
```go
// Add auth endpoints to router
func SetupRoutes(collector *collector.Collector, store *storage.Storage, cfg *config.Config) *http.ServeMux {
  mux := http.NewServeMux()
  
  // Session store
  var sessionStore auth.SessionStore
  if cfg.Auth.SessionStorage == "file" {
    sessionStore = auth.NewFileStore(cfg.Auth.StoragePath)
  } else {
    sessionStore = auth.NewMemoryStore()
  }
  
  // Public routes (no auth required)
  mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
    serveEmbeddedFile(w, "web/login.html", "text/html")
  })
  
  mux.HandleFunc("POST /api/auth/login", 
    securityHeaders(handleLogin(cfg, sessionStore)))
  
  // Protected routes (auth required if cfg.Auth.Enable)
  protected := func(handler http.Handler) http.Handler {
    return authRequired(cfg, sessionStore)(securityHeaders(handler))
  }
  
  mux.Handle("GET /api/members",
    protected(http.HandlerFunc(handleMembers(collector))))
  
  mux.Handle("GET /api/current",
    protected(http.HandlerFunc(handleCurrent(collector))))
  
  // ... rest of routes
  
  return mux
}

// Middleware: Check session validity
func authRequired(cfg *config.Config, store auth.SessionStore) func(http.Handler) http.Handler {
  return func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      if !cfg.Auth.Enable {
        next.ServeHTTP(w, r)
        return
      }
      
      // Skip auth check for public endpoints
      if r.URL.Path == "/login" || r.URL.Path == "/api/auth/login" {
        next.ServeHTTP(w, r)
        return
      }
      
      // Check session cookie
      cookie, err := r.Cookie("etcdmonitor_session")
      if err != nil {
        http.Redirect(w, r, "/login", http.StatusFound)
        return
      }
      
      session, err := store.Get(cookie.Value)
      if err != nil {
        http.SetCookie(w, &http.Cookie{
          Name:   "etcdmonitor_session",
          MaxAge: -1,  // Delete cookie
        })
        http.Redirect(w, r, "/login", http.StatusFound)
        return
      }
      
      // Session valid, continue
      r.Header.Set("X-Auth-Username", session.Username)
      next.ServeHTTP(w, r)
    })
  }
}
```

### Handler: Login (handler.go)
```go
func handleLogin(cfg *config.Config, store auth.SessionStore) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
      http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
      return
    }
    
    var req struct {
      Username string `json:"username"`
      Password string `json:"password"`
      Remember bool   `json:"remember"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
      http.Error(w, "Invalid request", http.StatusBadRequest)
      return
    }
    
    // Verify credentials against etcd
    authenticator := auth.NewAuthenticator(cfg)
    if err := authenticator.VerifyCredentials(req.Username, req.Password); err != nil {
      logger.Warnf("[Auth] Login failed for user %s: %v", req.Username, err)
      w.WriteHeader(http.StatusUnauthorized)
      json.NewEncoder(w).Encode(map[string]string{
        "error": "Invalid username or password",
      })
      return
    }
    
    // Create session
    duration := time.Duration(cfg.Auth.SessionTimeout) * time.Second
    if req.Remember {
      duration = time.Duration(cfg.Auth.RememberDuration) * time.Second
    }
    
    session, err := store.Create(req.Username, duration, req.Remember)
    if err != nil {
      logger.Errorf("[Auth] Session creation failed: %v", err)
      http.Error(w, "Login failed", http.StatusInternalServerError)
      return
    }
    
    // Set secure cookie
    http.SetCookie(w, &http.Cookie{
      Name:     "etcdmonitor_session",
      Value:    session.Token,
      Path:     "/",
      Expires:  session.ExpiresAt,
      HttpOnly: true,
      Secure:   cfg.Server.TLSEnable,  // Only over HTTPS in production
      SameSite: http.SameSiteLaxMode,
    })
    
    logger.Infof("[Auth] User %s logged in successfully", req.Username)
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
      "session_token": session.Token,
      "username":      session.Username,
      "expires_at":    session.ExpiresAt,
      "remember_me":   session.RememberMe,
    })
  }
}
```

## Security Analysis

### Session Token Security
- **Generation:** 32 bytes (256 bits) random data via `crypto/rand`
- **Format:** Hex-encoded string (64 characters)
- **Storage:** In-memory map or JSON files (never transmitted unnecessarily)
- **Transmission:** Only via httpOnly cookie (inaccessible to JavaScript)

### Cookie Security
```
Name: etcdmonitor_session
Value: <64-char hex token>
HttpOnly: true        # Prevent XSS access
Secure: true          # HTTPS only (when TLS enabled)
SameSite: Lax         # CSRF protection
Path: /               # Available to all paths
Expires: <1-7 days>   # Session duration
```

### Credential Verification
- **Never Stored:** Credentials only used once for verification
- **Verified Against:** etcd /metrics endpoint via BasicAuth
- **Timeout:** 5-second request timeout to prevent hanging
- **Logging:** Username logged, password never logged

### Brute Force Protection (Not Implemented in Phase 1)
- **Future Enhancement:** Rate limit /api/auth/login to 5 attempts per minute per IP
- **Consideration:** Could use middleware wrapping /api/auth/login

### Session Hijacking Prevention
- **HTTPS Only:** Secure flag prevents transmission over HTTP
- **HttpOnly Cookie:** JavaScript cannot read token
- **SameSite Cookie:** Prevents CSRF attacks
- **Token Expiration:** Automatic cleanup of expired sessions
- **Per-Request Validation:** Each request re-validates token freshness

## Configuration Examples

### Disabled (Default)
```yaml
auth:
  enable: false
```
Result: Dashboard loads immediately, no login page

### Simple In-Memory Sessions
```yaml
auth:
  enable: true
  session_timeout: 3600        # 1 hour
  remember_duration: 604800    # 7 days
  session_storage: memory      # Default
```
Result: Login page appears, sessions stored in RAM (lost on restart)

### Persistent File-Based Sessions
```yaml
auth:
  enable: true
  session_timeout: 1800        # 30 minutes
  remember_duration: 259200    # 3 days
  session_storage: file
  storage_path: /var/lib/etcdmonitor/sessions
```
Result: Login page appears, sessions persist across restarts

## Testing Checklist

### Unit Tests
- [ ] Session token generation uniqueness
- [ ] Session expiration detection
- [ ] MemoryStore: create, get, delete operations
- [ ] FileStore: JSON serialization, file cleanup
- [ ] Authenticator: credential verification logic

### Integration Tests
- [ ] Login with valid credentials → session created → dashboard loads
- [ ] Login with invalid credentials → 401 error displayed
- [ ] Session expires → redirect to login on next request
- [ ] Logout → session deleted → login required
- [ ] Remember me → session extends to 7 days
- [ ] Concurrent requests with same token → both succeed
- [ ] Multiple browser tabs → one tab logout → other tab redirected

### Manual Tests
- [ ] Login form responsive on mobile
- [ ] Error messages clear and helpful
- [ ] Theme preference remembered across login
- [ ] Password field hidden from screen readers
- [ ] Tab key navigation through form works

### Security Tests
- [ ] Cookie flags verified (HttpOnly, Secure, SameSite)
- [ ] Password never appears in logs
- [ ] HTTPS enforced when TLS enabled
- [ ] CORS headers respect localhost-only policy
- [ ] No sensitive data in browser console errors

