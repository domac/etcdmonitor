# Proposal: Add etcd Authentication Login UI to etcdmonitor

## Summary

Add a built-in authentication login page to etcdmonitor's web dashboard for scenarios where etcd clusters are configured with username/password authentication. Currently, credentials are only configurable via `config.yaml` and cannot be changed without restarting the service. This proposal introduces a secure, optional login UI that allows runtime credential management.

## Problem Statement

**Current Limitations:**
1. Authentication credentials hardcoded in `config.yaml` - requires restart to change
2. No UI for managing credentials - CLI/file editing only
3. Multi-team scenarios unsupported - one set of credentials per instance
4. No clear visual feedback when authentication fails - dashboard silently shows "no data"
5. Single-tenant by design - cannot support user-specific access control

**Impact:**
- Operational friction when rotating credentials
- Inability to use etcdmonitor in shared environments
- No audit trail for credential changes
- Difficult to troubleshoot authentication issues

## Solution Overview

Implement an **optional** authentication layer with:

1. **Login Page** - Clean UI prompting for username/password when auth is configured
2. **Session Management** - Secure HTTP cookies (httpOnly, secure, sameSite)
3. **Credential Storage** - In-memory or optional file-based persistent storage
4. **API Authentication** - All endpoints protected by session token validation
5. **Visual Feedback** - Clear error messages and login status indicators

### Key Design Principles

- **Backward Compatible** - Existing deployments without auth config continue working unchanged
- **Optional** - Feature only activates when explicitly enabled
- **Secure by Default** - No plain-text credential exposure, HTTPS recommended
- **Stateless Fallback** - Graceful degradation if session storage unavailable
- **Zero Trust Endpoints** - All APIs validate authentication before responding

## Non-Goals

- Multi-user role-based access control (RBAC) - out of scope
- Single sign-on (SSO) / OAuth2 integration - future enhancement
- Per-metric access control - all authenticated users see all metrics
- Password strength requirements - delegated to etcd's auth policy

## Technical Design

### 1. Frontend Changes

#### New Components
- **LoginPage** (`web/login.html` + `web/login.js`)
  - Username/password form
  - "Remember me" checkbox (optional)
  - Submit to `/api/auth/login` endpoint
  - Error message display
  - Loading state during submission

- **Modified app.js**
  - Check session validity on page load
  - Redirect to login if session expired
  - Add auth headers to all API calls
  - Logout button in header

#### Login Page Features
```html
<!-- Minimal, clean UI -->
<div class="login-container">
  <div class="login-form">
    <h1>etcd Monitor</h1>
    <p>Requires authentication</p>
    <form id="loginForm">
      <input type="text" placeholder="Username" required>
      <input type="password" placeholder="Password" required>
      <label><input type="checkbox"> Remember for 7 days</label>
      <button type="submit">Sign In</button>
    </form>
    <p class="error-message" style="display:none;"></p>
  </div>
</div>
```

### 2. Backend Changes

#### New Configuration (`config.yaml`)
```yaml
auth:
  enable: false                    # false: disable auth UI, true: show login page
  session_timeout: 3600           # seconds (1 hour)
  remember_duration: 604800       # seconds (7 days) - remember me checkbox
  session_storage: memory         # "memory" or "file"
  storage_path: "data/sessions"   # for session persistence
```

#### New API Endpoints
- `POST /api/auth/login`
  - Input: `{username, password, remember}`
  - Output: `{session_token, expires_at}`
  - Sets secure httpOnly cookie
  - Returns 401 if credentials invalid

- `POST /api/auth/logout`
  - Clears session cookie
  - Invalidates token

- `GET /api/auth/status`
  - Returns current session info
  - `{authenticated, username, expires_at}`

- `POST /api/auth/refresh`
  - Extends session duration if "remember me" enabled
  - Returns updated expiry

#### Session Management Implementation
```go
// internal/auth/session.go (new file)
type Session struct {
    Token       string
    Username    string
    ExpiresAt   time.Time
    RememberMe  bool
}

type SessionStore interface {
    Create(username string, rememberMe bool) (Session, error)
    Get(token string) (Session, error)
    Delete(token string) error
    Cleanup() // Remove expired sessions
}

// Two implementations:
// 1. MemoryStore - in-memory map with mutex
// 2. FileStore - JSON files in data/sessions, auto-cleanup on startup
```

#### Middleware Integration
```go
// New middleware in api.go
func authRequired(cfg *config.Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !cfg.Auth.Enable {
                next.ServeHTTP(w, r)
                return
            }
            
            // Check session cookie
            token, _ := r.Cookie("etcdmonitor_session")
            if token == nil || !validateSession(token.Value) {
                http.Redirect(w, r, "/login", http.StatusFound)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### 3. Public vs Protected Routes

**Public (No Auth Required):**
- `GET /login` - Login page HTML
- `POST /api/auth/login` - Credential verification
- `GET /` - Redirects to `/login` or dashboard based on auth status
- Health check endpoint (optional)

**Protected (Auth Required if `auth.enable: true`):**
- All `/api/*` endpoints (members, current, range, status, debug)
- Dashboard HTML (after login redirect)

### 4. Security Considerations

| Aspect | Implementation |
|--------|-----------------|
| **Credential Transport** | HTTPS only (config.tls_enable: true recommended) |
| **Credential Storage** | Never stored; verified against etcd directly via /metrics endpoint |
| **Session Tokens** | Random 32-byte hex, cryptographically secure |
| **Cookie Flags** | httpOnly=true, Secure=true (if TLS), SameSite=Lax |
| **Password Exposure** | Only sent once to POST /api/auth/login, never logged |
| **CSRF Protection** | SameSite cookie attribute + Origin header validation |
| **Session Cleanup** | Auto-cleanup on startup, hourly for expired sessions |
| **Brute Force** | No protection (future enhancement: rate limiting) |

### 5. Backward Compatibility

**Scenarios:**

| Scenario | Behavior |
|----------|----------|
| `auth.enable: false` (default) | No changes; dashboard loads normally; login endpoints return 400 |
| `auth.enable: true`, etcd has no auth | Login always fails with "authentication required by etcd" message |
| `auth.enable: true`, valid credentials provided | Dashboard requires login, session token validated per request |
| Existing deployments | Zero changes required; feature opt-in via config |

## Implementation Phases

### Phase 1: Backend (Session Management)
- [ ] Add session.go with MemoryStore + FileStore implementations
- [ ] Add auth config section to Config struct
- [ ] Implement /api/auth/login, /api/auth/logout, /api/auth/status endpoints
- [ ] Add authRequired middleware to all protected routes
- [ ] Add session cleanup background loop

**Estimated effort:** 6 hours
**Files affected:** config.go, api.go, handler.go, session.go (new)

### Phase 2: Frontend (Login UI + Session Handling)
- [ ] Create login.html with responsive form
- [ ] Create login.js with form submission + error handling
- [ ] Modify app.js to check session on load + redirect to login if needed
- [ ] Add logout button + session status to dashboard header
- [ ] Add Authorization header to all fetch() calls

**Estimated effort:** 4 hours
**Files affected:** login.html (new), login.js (new), app.js, index.html

### Phase 3: Integration & Testing
- [ ] End-to-end testing: login flow, session expiry, logout
- [ ] Test backward compatibility: auth.enable: false scenarios
- [ ] Test "remember me" checkbox functionality
- [ ] Test concurrent requests with same token
- [ ] Security review: HTTPS enforcement, cookie flags, CSRF

**Estimated effort:** 3 hours
**Files affected:** all

### Phase 4: Documentation & Deployment
- [ ] Update README.md with auth configuration section
- [ ] Add examples: enabling auth, rotating credentials
- [ ] Update config.yaml template with auth section
- [ ] Test install.sh + systemd integration
- [ ] Create migration guide for existing users

**Estimated effort:** 2 hours
**Files affected:** README.md, config.yaml, install.sh

**Total estimated effort:** 15 hours (2 developer days)

## Testing Strategy

### Unit Tests
- Session token generation and validation
- Session expiry logic
- MemoryStore and FileStore operations
- Credential validation flow

### Integration Tests
- Login flow: valid/invalid credentials
- Session persistence across requests
- Session expiry and refresh
- Logout clears session
- API endpoints reject invalid tokens

### Manual Tests
- Browser login experience (responsive design)
- "Remember me" checkbox behavior
- Session timeout during idle
- Concurrent dashboard tabs
- HTTPS browser security warnings (optional)

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Session token theft | Low | High | HTTPS + httpOnly cookies + SameSite |
| Credentials logged | Medium | High | Never log passwords; sanitize logs |
| Auth bypass via header injection | Low | Medium | Strict header parsing + Origin validation |
| Session storage corruption | Low | Medium | In-memory default; file store has recovery |
| Performance impact | Low | Low | Session validation is O(1) map lookup |

## Future Enhancements

1. **Rate Limiting** - Prevent brute-force login attempts
2. **Multi-Tenant** - Support multiple etcd credentials with user selection
3. **RBAC** - Per-user metric access control
4. **SSO Integration** - OIDC / LDAP for enterprise environments
5. **Audit Logging** - Track login attempts, credential changes
6. **Password Rotation** - Scheduled credential refresh from external vault

## Success Criteria

1. ✅ Login page appears when `auth.enable: true` in config.yaml
2. ✅ Invalid credentials show clear error message
3. ✅ Valid credentials create secure session
4. ✅ Dashboard inaccessible without valid session
5. ✅ Session persists across browser tabs/refreshes
6. ✅ Logout clears session immediately
7. ✅ "Remember me" extends session to 7 days
8. ✅ All existing deployments work unchanged (auth.enable defaults to false)
9. ✅ No performance degradation in non-auth scenarios
10. ✅ HTTPS + secure cookies recommended in documentation

## Open Questions

1. Should "remember me" be optional or always available?
   - **Decision:** Optional checkbox, default unchecked
   
2. Session storage default: memory or file?
   - **Decision:** Memory by default; file option for multi-instance setups
   
3. Should login page inherit dashboard theme?
   - **Decision:** Yes, read localStorage for user's preferred theme before authentication
   
4. What if etcd /metrics endpoint requires different auth than cluster?
   - **Decision:** Out of scope; assume single auth method per instance
   
5. Multi-instance load-balanced deployment support?
   - **Decision:** File-based sessions with shared NFS mount (documented limitation)

## Approval Checklist

- [ ] Core team review and approval
- [ ] Security review (encryption, HTTPS enforcement, cookie flags)
- [ ] UX review (login form accessibility, error messages)
- [ ] Documentation review (config examples, troubleshooting)
- [ ] Go code standards review (error handling, logging)

