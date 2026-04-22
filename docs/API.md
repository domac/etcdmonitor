← Back to [README](../README.md)

# API Reference

Complete HTTP API surface exposed by etcdmonitor. All protected endpoints
require a valid session (set via `Authorization: Bearer <token>` after
logging in through `/api/auth/login`). The only unauthenticated endpoints
are the three under `/api/auth/` and the healthcheck (where applicable).

## Dashboard APIs

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/auth/login` | POST | No | Local admin login. Response may contain `must_change_password=true` (then no session is issued) |
| `/api/auth/change-password` | POST | No | Change password (zero-token: authorized by `username + old_password`) |
| `/api/auth/logout` | POST | Yes | Logout and invalidate session |
| `/api/auth/status` | GET | No | Check auth requirement and session status |
| `/api/members` | GET | Yes | List all cluster members |
| `/api/current?member_id=<id>` | GET | Yes | Latest metrics snapshot for a member |
| `/api/range?member_id=<id>&metrics=m1,m2&range=1h` | GET | Yes | Time-series data for specified metrics |
| `/api/status` | GET | Yes | Monitor system status & cluster info |
| `/api/user/panel-config` | GET | Yes | Get user's panel visibility & order config |
| `/api/user/panel-config` | PUT | Yes | Save user's panel visibility & order config |
| `/api/debug` | GET | Yes | Debug info: DB member IDs, collector state |

## KV Management APIs (v3)

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/kv/v3/connect` | POST | Yes | Connect and get cluster info (version, leader, DB size) |
| `/api/kv/v3/separator` | GET | Yes | Get the key path separator |
| `/api/kv/v3/keys` | GET | Yes | Get full key tree structure (keys only, no values) |
| `/api/kv/v3/getpath?key=/` | GET | Yes | Get key tree under a path (recursive) |
| `/api/kv/v3/get?key=/foo` | GET | Yes | Get a single key's value and metadata |
| `/api/kv/v3/put` | PUT | Yes | Create or update a key (JSON body: `key`, `value`, `ttl`) |
| `/api/kv/v3/delete` | POST | Yes | Delete a key or directory (JSON body: `key`, `dir`) |

## KV Management APIs (v2)

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/kv/v2/connect` | POST | Yes | Connect and check v2 API availability |
| `/api/kv/v2/separator` | GET | Yes | Get the key path separator |
| `/api/kv/v2/keys` | GET | Yes | Get full key tree structure (keys only, no values) |
| `/api/kv/v2/getpath?key=/` | GET | Yes | Get key tree under a path (recursive) |
| `/api/kv/v2/get?key=/foo` | GET | Yes | Get a single key's value and metadata |
| `/api/kv/v2/put` | PUT | Yes | Create or update a key (JSON body: `key`, `value`, `ttl`, `dir`) |
| `/api/kv/v2/delete` | POST | Yes | Delete a key or directory (JSON body: `key`, `dir`) |

## Ops APIs

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/ops/defragment` | POST | Yes | Defragment a member (JSON body: `member_id`) |
| `/api/ops/snapshot` | GET | Yes | Download snapshot from a member (query: `member_id`) |
| `/api/ops/alarms` | GET | Yes | List active cluster alarms |
| `/api/ops/alarms/disarm` | POST | Yes | Disarm a specific alarm (JSON body: `member_id`, `alarm_type`) |
| `/api/ops/move-leader` | POST | Yes | Move leader to target member (JSON body: `target_member_id`) |
| `/api/ops/hashkv` | POST | Yes | Run HashKV consistency check across all members |
| `/api/ops/compact` | POST | Yes | Cluster-wide revision compaction (JSON body: `retain_count`, `physical`) |
| `/api/ops/compact/revision` | GET | Yes | Get current cluster Revision for reference |
| `/api/ops/audit-logs` | GET | Yes | Query audit logs (query: `page`, `page_size`, `operation`) |

## Authentication notes

- **Local admin only.** Dashboard login is decoupled from etcd auth —
  `etcd.username` / `etcd.password` in `config.yaml` are used only by
  Collector / KV Manager / Ops SDK clients.
- **Session.** Login issues a session token in the response body; include it
  as `Authorization: Bearer <token>` for every protected request.
- **Lockout.** 5 consecutive failures (shared between login and
  change-password) lock the account for 15 minutes. See
  [CONFIGURATION.md](./CONFIGURATION.md) `auth:` block to tune.
- **First login.** When `must_change_password=true` is returned, no session
  is issued — the client must POST to `/api/auth/change-password` with the
  initial password to proceed.

## Related documents

- [Configuration reference](./CONFIGURATION.md)
- [TLS setup](./TLS.md)
- [Security checklist](./SECURITY_CHECKLIST.md)
- [Back to README](../README.md)

<!-- Keep this file in sync with the main README; any change to an API endpoint must be reflected here. -->
