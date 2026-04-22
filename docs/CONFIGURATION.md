← Back to [README](../README.md)

# Configuration Reference

This document is the **full configuration reference** for etcdmonitor.
The main [README](../README.md) keeps only a minimal snippet — this file is the
source of truth for every option.

## Full `config.yaml`

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"         # Supports comma-separated multi-endpoints for failover
  username: ""                              # Collector credentials (leave empty if no auth)
  password: ""
  metrics_path: "/metrics"
  tls_enable: false                         # true: connect to etcd via TLS
  tls_cert: "certs/client.crt"              # Client certificate (for mTLS)
  tls_key: "certs/client.key"               # Client private key (for mTLS)
  tls_ca_cert: "certs/ca.crt"               # CA certificate (to verify etcd server)
  tls_insecure_skip_verify: false           # Skip server cert verification (testing only)
  tls_server_name: ""                       # Server name for SNI verification

server:
  listen: ":9090"
  tls_enable: false                         # true: HTTPS, false: HTTP
  tls_cert: "certs/server.crt"
  tls_key: "certs/server.key"
  session_timeout: 3600                     # Dashboard session timeout (seconds)

collector:
  interval: 30          # seconds

storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7

kv_manager:
  separator: "/"                            # Key path separator for tree view
  connect_timeout: 5                        # etcd connection timeout (seconds)
  request_timeout: 30                       # etcd request timeout (seconds)
  max_value_size: 2097152                   # Max value size in bytes (default 2MB)

ops:
  ops_enable: true                          # Enable Ops panel (set false to hide Ops tab and block /api/ops/*)
  audit_retention_days: 7                   # Audit log retention period (days), auto-cleanup on expiry

auth:
  bcrypt_cost: 10                           # Password hash cost (8-14; out-of-range falls back to 10 with WARN)
  lockout_threshold: 5                      # Lock account after N consecutive failures (shared by login & change-password)
  lockout_duration_seconds: 900             # Lockout duration (seconds), default 15 minutes
  min_password_length: 8                    # Minimum new-password length

log:
  dir: "logs"
  filename: "etcdmonitor.log"
  level: "info"                             # debug, info, warn, error
  max_size_mb: 50                           # Max single log file size (MB)
  max_files: 5                              # Max number of log files to keep
  max_age: 30                               # Max days to retain old logs (0 = no age limit)
  compress: false                           # Compress rotated log files (gzip)
  console: true                             # Also output to console
```

## Parameter reference

| Parameter | Description | Default |
|---|---|---|
| `etcd.endpoint` | etcd client endpoint (comma-separated for multi-endpoint failover) | `http://127.0.0.1:2379` |
| `etcd.username` | Collector auth username (leave empty if no auth) | — |
| `etcd.password` | Collector auth password | — |
| `etcd.tls_enable` | Enable TLS for etcd client connections | `false` |
| `etcd.tls_cert` | Client certificate file path (for mTLS) | `certs/client.crt` |
| `etcd.tls_key` | Client private key file path (for mTLS) | `certs/client.key` |
| `etcd.tls_ca_cert` | CA certificate file path (to verify etcd server) | `certs/ca.crt` |
| `etcd.tls_insecure_skip_verify` | Skip server certificate verification (testing only) | `false` |
| `etcd.tls_server_name` | Server name for SNI verification | — |
| `server.listen` | Dashboard listen address | `:9090` |
| `server.tls_enable` | Enable HTTPS | `false` |
| `server.tls_cert` | TLS certificate file path | `certs/server.crt` |
| `server.tls_key` | TLS private key file path | `certs/server.key` |
| `server.session_timeout` | Dashboard login session timeout (seconds), 0 = no expiry | `3600` |
| `collector.interval` | Metrics collection interval (seconds) | `30` |
| `storage.db_path` | SQLite database file path | `data/etcdmonitor.db` |
| `storage.retention_days` | Data retention period (days) | `7` |
| `kv_manager.separator` | Key path separator for KV tree view | `/` |
| `kv_manager.connect_timeout` | etcd connection timeout for KV operations (seconds) | `5` |
| `kv_manager.request_timeout` | etcd request timeout for KV operations (seconds) | `30` |
| `kv_manager.max_value_size` | Max value size for KV operations (bytes) | `2097152` (2MB) |
| `ops.ops_enable` | Enable Ops panel; when `false`, Ops tab is hidden and `/api/ops/*` returns 403 | `true` |
| `ops.audit_retention_days` | Audit log retention period (days), expired entries auto-purged | `7` |
| `auth.bcrypt_cost` | Password hash cost (8–14; out-of-range falls back to 10 with WARN) | `10` |
| `auth.lockout_threshold` | Lock account after N consecutive failures (shared by login & change-password) | `5` |
| `auth.lockout_duration_seconds` | Lockout duration (seconds) | `900` |
| `auth.min_password_length` | Minimum new-password length | `8` |
| `log.dir` | Log file directory | `logs` |
| `log.filename` | Log file name | `etcdmonitor.log` |
| `log.level` | Log level: `debug`, `info`, `warn`, `error` | `info` |
| `log.max_size_mb` | Max single log file size before rotation (MB) | `50` |
| `log.max_files` | Max number of rotated log files to keep | `5` |
| `log.max_age` | Max days to retain old log files (0 = no age limit) | `30` |
| `log.compress` | Compress rotated log files with gzip | `false` |
| `log.console` | Also output logs to console (stdout) | `true` |

## Related documents

- [TLS configuration](./TLS.md) — Dashboard HTTPS & etcd mTLS setup
- [API reference](./API.md) — HTTP endpoints protected by auth
- [Data retention](./DATA_RETENTION.md) — Storage, retention policy, downsampling
- [Security checklist](./SECURITY_CHECKLIST.md) — Pre-production hardening
- [Back to README](../README.md)

<!-- Keep this file in sync with the main README; any change to a config key must be reflected here. -->
