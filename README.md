<div align="right">

**English** | [简体中文](README_CN.md)

</div>

<div align="center">

# etcd Monitor

**Lightweight, self-contained monitoring dashboard for etcd clusters.**

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![etcd](https://img.shields.io/badge/etcd-3.4.x-419EDA?logo=etcd&logoColor=white)](https://etcd.io)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20x86__64-lightgrey)](https://github.com)

Single binary. Zero dependencies. No Prometheus. No Grafana.

![etcd Monitor Dashboard](./dashboard.png)

</div>

---

## Features

- **Zero dependencies** - Single static binary (~31MB), embeds web UI, SQLite storage, everything
- **Multi-member cluster** - Auto-discovers all etcd members via official Go SDK, concurrent metrics collection
- **Multi-endpoint failover** - Comma-separated endpoints with global health management, auto-recovery
- **KV Tree management** - Browse, create, edit, delete keys with tree/list view, supports etcd v3 & v2
- **KV Tree search** - Real-time key filtering with hierarchy preservation, 60s background index refresh
- **Ops panel** - Cluster operations center: Defragment, Snapshot, Alarms, Move Leader, HashKV consistency check, Audit Log with sortable table, pagination, and CSV export
- **80+ metrics, 25 charts** - Covers Raft, disk I/O, MVCC, Lease, network, gRPC, Go runtime
- **Dashboard login** - Auto-detects etcd auth; when enabled, operators must log in with etcd credentials
- **Panel configuration** - Show/hide and drag-to-reorder monitoring panels, per-user persistent settings
- **Dark / Light theme** - Toggle with one click, preference saved in browser
- **HTTPS support** - Optional TLS with bundled self-signed certificates, or bring your own
- **etcd TLS/mTLS** - Connect to TLS-secured etcd clusters with client certificates (CA + cert + key)
- **Auto downsampling** - Smart query aggregation keeps dashboard responsive even with 7 days of data
- **One-click deploy** - `install.sh` sets up systemd service, `uninstall.sh` cleans up
- **Smart endpoint detection** - Supports `127.0.0.1` config, auto-resolves to real member ID
- **Data isolation** - Per-member storage in SQLite, auto-cleanup on cluster endpoint change
- **Data retention** - Configurable retention period (default 7 days), automatic expired data purge with disk reclaim

## Quick Start

### Download & Deploy

```bash
# Upload to server

unzip etcdmonitor-v0.5.0-linux-amd64.zip
cd etcdmonitor-v0.5.0-linux-amd64

# Edit config
vim config.yaml

# Install & start
sh install.sh
```

Open `http://<server-ip>:9090` in your browser.

### Build from Source

```bash
git clone https://github.com/domac/etcdmonitor.git
cd etcdmonitor

# Build binary only (for development/testing)
./build.sh

# Create full deployment package (binary + config + certs + install scripts)
./package.sh
# Output: dist/etcdmonitor-v<version>-linux-amd64.zip
```

**Requirements:** Go 1.21+

## Configuration

Edit `config.yaml`:

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"         # Supports comma-separated multi-endpoints for failover
  username: ""                              # Collector credentials (leave empty if no auth)
  password: ""
  metrics_path: "/metrics"
  tls_enable: false                         # true: connect to etcd via TLS
  tls_cert: "certs/client.crt"              # Client certificate (for mTLS)
  tls_key: "certs/client.key"               # Client private key (for mTLS)
  tls_ca_cert: "certs/ca.crt"              # CA certificate (to verify etcd server)
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

| Parameter | Description | Default |
|---|---|---|
| `etcd.endpoint` | etcd client endpoint (comma-separated for multi-endpoint failover) | `http://127.0.0.1:2379` |
| `etcd.username` | Collector auth username (leave empty if no auth) | - |
| `etcd.password` | Collector auth password | - |
| `etcd.tls_enable` | Enable TLS for etcd client connections | `false` |
| `etcd.tls_cert` | Client certificate file path (for mTLS) | `certs/client.crt` |
| `etcd.tls_key` | Client private key file path (for mTLS) | `certs/client.key` |
| `etcd.tls_ca_cert` | CA certificate file path (to verify etcd server) | `certs/ca.crt` |
| `etcd.tls_insecure_skip_verify` | Skip server certificate verification (testing only) | `false` |
| `etcd.tls_server_name` | Server name for SNI verification | - |
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
| `log.dir` | Log file directory | `logs` |
| `log.filename` | Log file name | `etcdmonitor.log` |
| `log.level` | Log level: debug, info, warn, error | `info` |
| `log.max_size_mb` | Max single log file size before rotation (MB) | `50` |
| `log.max_files` | Max number of rotated log files to keep | `5` |
| `log.max_age` | Max days to retain old log files (0 = no age limit) | `30` |
| `log.compress` | Compress rotated log files with gzip | `false` |
| `log.console` | Also output logs to console (stdout) | `true` |

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                   etcdmonitor (single binary)                  │
│                                                                │
│  ┌───────────┐   ┌──────────┐   ┌────────────────────────┐   │
│  │ Collector  │──▶│  SQLite  │◀──│  HTTP/S API            │   │
│  │ (30s poll) │   │ (per     │   │  /api/current          │   │
│  │ concurrent │   │  member)  │   │  /api/range            │   │
│  └─────┬──┬──┘   └──────────┘   │  /api/members          │   │
│        │  │                      │  /api/status            │   │
│  ┌─────┴──┴──┐   ┌──────────┐   │  /api/auth/*           │   │
│  │  Health   │   │ User     │◀──│  /api/user/*           │   │
│  │  Manager  │   │ Prefs    │   │  /api/kv/v3/*          │   │
│  │ (15s chk) │   │ (JSON)   │   │  /api/kv/v2/*          │   │
│  └─────┬─────┘   └──────────┘   │  /api/ops/*            │   │
│        │                         └──────────┬─────────────┘   │
│        │                                     │                │
│  ┌─────▼───────┐   ┌──────────┐   ┌────────▼──────────────┐ │
│  │    etcd     │   │KV Manager│──▶│  Dashboard             │ │
│  │  /metrics   │   │ v3 + v2  │   │  Monitor + KV Tree     │ │
│  │  x N nodes  │   │per-request│  │  Ops Panel + Login     │ │
│  └─────────────┘   └──────────┘   └────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

## Dashboard Panels

### Key Metrics Banner

| Metric | Source | Description |
|---|---|---|
| CPU Usage | `process_cpu_seconds_total` (rate) | Real-time CPU usage percentage |
| Memory | `process_resident_memory_bytes` | Resident memory (RSS) |
| DB Size | `etcd_mvcc_db_total_size_in_bytes` | Database total / in-use size |
| KV Total | `etcd_debugging_mvcc_keys_total` | Total key-value revisions |
| Backend Commit P99 | `etcd_disk_backend_commit_duration_seconds` | boltdb commit latency |

### Overview Cards

| Card | Alert Condition |
|---|---|
| Leader Status | Red when no leader |
| Leader Changes | Watch for frequent changes |
| Members | Cluster size, hover for member details |
| WAL Fsync P99 | Red when > 10ms |
| Proposals Pending | Red when > 5 |
| Commit-Apply Lag | Red when > 50 |
| Proposal Failed Rate | Red when > 0/s |

### Chart Panels (25 charts, 18 default + 7 extended)

| Section | Charts | Key Metrics |
|---|---|---|
| **Raft & Server** | Proposals, Leader Changes, Commit-Apply Lag, Failed Rate | `proposals_*`, `leader_changes`, `slow_apply` |
| **Raft & Server** *(ext)* | Server Health & Quota | `quota_backend_bytes`, `heartbeat_send_failures`, `health_*`, `client_requests` by API version |
| **Disk Performance** | WAL Fsync Duration, Backend Commit Duration | P50/P90/P99 latency histograms |
| **Disk Performance** *(ext)* | Snapshot & Defrag Duration, Backend Commit Breakdown | `defrag`/`snapshot`/`snap_db` latency, commit sub-phase `rebalance`/`spill`/`write` P50/P90/P99 |
| **MVCC & Storage** | Database Size, MVCC Operations | `db_total_size`, `put/delete/txn/range` totals |
| **MVCC & Storage** *(ext)* | MVCC Revisions & Compaction, Watcher & Events | `compact/current_revision`, `compaction_keys/duration`, `events_total`, `pending_events`, `watch_stream`, `slow_watcher` |
| **Lease Management** *(ext)* | Lease Activity | `lease_granted/revoked/renewed/expired` totals |
| **Network & Peers** | Peer Traffic, Peer RTT | `peer_sent/received_bytes`, RTT P50/P90/P99 |
| **Network & Peers** *(ext)* | Active Peers & gRPC Messages | `network_active_peers`, `grpc_server_msg_sent/received` |
| **gRPC Requests** | Request Rate, Client Traffic | `grpc_server_handled` (OK/Error), traffic bytes |
| **Process & Runtime** | CPU Usage, Memory, Goroutines, GC Duration, File Descriptors, Memory Sys | CPU %, RSS, heap, GC pause, FDs, sys memory |

> Panels marked *(ext)* are **hidden by default**. Enable them via the gear button (⚙) in the dashboard header.

## HTTPS / TLS

### Dashboard HTTPS

The install package includes a self-signed TLS certificate (valid for 1 year). To enable HTTPS:

```yaml
# config.yaml
server:
  tls_enable: true
```

Restart the service and access via `https://<server-ip>:9090`.

**Using your own certificate:**

Replace the files in the `certs/` directory:

```bash
cp /path/to/your/cert.crt certs/server.crt
cp /path/to/your/cert.key certs/server.key
systemctl restart etcdmonitor
```

**Regenerating self-signed certificate** (development only):

```bash
# In the source repository
./tools/gen-certs.sh          # default: 1 year
./tools/gen-certs.sh 730      # custom: 2 years
```

> Self-signed certificates will trigger a browser warning. Click "Advanced" > "Proceed" to continue, or use a certificate from a trusted CA for production.

### etcd Client TLS (mTLS)

If your etcd cluster is deployed with TLS (`client-transport-security` with `client-cert-auth: true`), etcd Monitor supports connecting via client certificates.

**Configuration:**

```yaml
etcd:
  endpoint: "https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379"
  tls_enable: true
  tls_cert: "certs/etcd-client.pem"         # Client certificate
  tls_key: "certs/etcd-client-key.pem"      # Client private key
  tls_ca_cert: "certs/ca.pem"               # CA certificate
```

**Setup steps:**

```bash
# Copy etcd client certificates to the etcdmonitor certs directory
cp /etc/etcd/ssl/etcd-client.pem certs/etcd-client.pem
cp /etc/etcd/ssl/etcd-client-key.pem certs/etcd-client-key.pem
cp /etc/etcd/ssl/ca.pem certs/ca.pem

# Edit config.yaml (set tls_enable: true and certificate paths)
vim config.yaml

# Restart the service
systemctl restart etcdmonitor
```

**Supported scenarios:**

| Scenario | `tls_enable` | `tls_cert` / `tls_key` | `tls_ca_cert` | `username` / `password` |
|---|---|---|---|---|
| Plain HTTP (no auth) | `false` | - | - | - |
| Plain HTTP + password auth | `false` | - | - | ✅ |
| HTTPS + CA only (server verification) | `true` | - | ✅ | Optional |
| HTTPS + mTLS (client cert) | `true` | ✅ | ✅ | Optional |
| HTTPS + mTLS + password auth | `true` | ✅ | ✅ | ✅ |

> **Note:** When `tls_enable: true`, the endpoint must use `https://`. TLS is applied to all etcd connections: health probes, metrics collection, member discovery, KV management, and authentication.

## Multi-Member Cluster Support

etcd Monitor automatically discovers all cluster members via the official etcd v3 Go SDK (`clientv3.MemberList()`) and collects metrics concurrently.

- No external binary dependencies (no `etcdctl` required)
- Members are refreshed every 60 seconds (handles scaling events)
- Each member's metrics are stored with its own `member_id` in SQLite
- Dashboard header has a dropdown to switch between members
- Local node is auto-detected even when config uses `127.0.0.1`

## Multi-Endpoint Failover

Configure multiple etcd endpoints for high availability:

```yaml
etcd:
  endpoint: "http://10.0.1.1:2379,http://10.0.1.2:2379,http://10.0.1.3:2379"
```

- **Startup probe** - All endpoints are probed in parallel at startup; unhealthy ones are excluded
- **Background health check** - Every 15 seconds, all endpoints are re-checked; recovered endpoints automatically rejoin
- **Global healthy list** - Collector, KV Manager, and Auth modules all share the healthy endpoint list
- **All-dead protection** - If all endpoints become unreachable, the process exits with a clear log message
- Fully backward-compatible: single endpoint config works as before

## KV Tree Management

Built-in key-value browser and editor, accessible via the **KV Tree** tab in the dashboard header.

- **Dual protocol** - Supports both etcd v3 (gRPC) and v2 (HTTP) APIs, toggle with one click
- **Tree view** - Hierarchical tree with expand/collapse, dashed guide lines, SVG folder/file icons
- **List view** - Flat key listing with full paths
- **Root node** - The `/` root is always visible, right-click to create keys at the top level
- **CRUD operations** - Create, read, update, delete keys via context menu and editor panel
- **Key search & filter** - Real-time key filtering in tree panel; case-insensitive substring match; matched directories expand all children; hierarchy preserved; search state resets on protocol switch
- **Keys-only loading** - Initial tree load and 60s background refresh use keys-only API (no value transfer), values loaded on-demand when clicking a node
- **ACE Editor** - Syntax highlighting for JSON, YAML, TOML, XML, and more; dark/light theme sync
- **TTL support** - Set time-to-live on keys; expired keys are automatically detected and removed from tree
- **Per-request client** - Each KV operation creates a temporary etcd connection (etcdkeeper pattern), no long-lived connections
- **Protocol state cache** - Switching between v3/v2 preserves each protocol's tree state

## Theme

Dashboard supports **Dark** and **Light** themes. Click the theme toggle button (☾ / ☀) in the top-right corner to switch. Your preference is saved in the browser's localStorage.

## Data Retention & Storage

### Retention Policy

- Data older than `retention_days` (default: **7 days**) is automatically purged
- Cleanup runs **every hour**
- When > 10,000 rows are deleted, a full `VACUUM` reclaims disk space
- Smaller deletions use `incremental_vacuum` for minimal overhead

### Auto Downsampling

Large time ranges are automatically downsampled to keep the dashboard responsive:

| Time Range | Aggregation | Data Points per Metric | Method |
|---|---|---|---|
| ≤ 30 min | None | ~60 | Raw data |
| ≤ 2 hours | 30 sec | ~240 | `AVG()` |
| ≤ 12 hours | 2 min | ~360 | `AVG()` |
| ≤ 48 hours | 5 min | ~576 | `AVG()` |
| > 48 hours | 10 min | ~1,008 | `AVG()` |

### Storage Estimates

| Cluster Size | 7-Day Rows | DB File Size |
|---|---|---|
| 1 node | ~1M | ~50 MB |
| 3 nodes | ~3M | ~150 MB |
| 5 nodes | ~5M | ~250 MB |

## Service Management

```bash
# Status
systemctl status etcdmonitor

# Start / Stop / Restart
systemctl start etcdmonitor
systemctl stop etcdmonitor
systemctl restart etcdmonitor

# Logs
journalctl -u etcdmonitor -f
tail -f logs/etcdmonitor.log
```

The service runs as `root` by default. To run as a different user, edit the `RUN_USER` variable at the top of `install.sh` before installation. The service auto-restarts on crash.

## Endpoint Change Detection

When `etcd.endpoint` in `config.yaml` changes (pointing to a different cluster), all historical data is automatically purged on restart to prevent data mixing. Switching between members in the dashboard does **not** trigger data cleanup.

## Uninstall

```bash
sudo ./uninstall.sh
```

Removes the systemd service. Optionally deletes data and logs (interactive prompt). Binary and config are preserved for re-installation.

## API Reference

### Dashboard APIs

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/auth/login` | POST | No | Login with etcd credentials |
| `/api/auth/logout` | POST | Yes | Logout and invalidate session |
| `/api/auth/status` | GET | No | Check auth requirement and session status |
| `/api/members` | GET | Yes | List all cluster members |
| `/api/current?member_id=<id>` | GET | Yes | Latest metrics snapshot for a member |
| `/api/range?member_id=<id>&metrics=m1,m2&range=1h` | GET | Yes | Time-series data for specified metrics |
| `/api/status` | GET | Yes | Monitor system status & cluster info |
| `/api/user/panel-config` | GET | Yes | Get user's panel visibility & order config |
| `/api/user/panel-config` | PUT | Yes | Save user's panel visibility & order config |
| `/api/debug` | GET | Yes | Debug info: DB member IDs, collector state |

### KV Management APIs (v3)

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/kv/v3/connect` | POST | Yes | Connect and get cluster info (version, leader, DB size) |
| `/api/kv/v3/separator` | GET | Yes | Get the key path separator |
| `/api/kv/v3/keys` | GET | Yes | Get full key tree structure (keys only, no values) |
| `/api/kv/v3/getpath?key=/` | GET | Yes | Get key tree under a path (recursive) |
| `/api/kv/v3/get?key=/foo` | GET | Yes | Get a single key's value and metadata |
| `/api/kv/v3/put` | PUT | Yes | Create or update a key (JSON body: key, value, ttl) |
| `/api/kv/v3/delete` | POST | Yes | Delete a key or directory (JSON body: key, dir) |

### KV Management APIs (v2)

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/kv/v2/connect` | POST | Yes | Connect and check v2 API availability |
| `/api/kv/v2/separator` | GET | Yes | Get the key path separator |
| `/api/kv/v2/keys` | GET | Yes | Get full key tree structure (keys only, no values) |
| `/api/kv/v2/getpath?key=/` | GET | Yes | Get key tree under a path (recursive) |
| `/api/kv/v2/get?key=/foo` | GET | Yes | Get a single key's value and metadata |
| `/api/kv/v2/put` | PUT | Yes | Create or update a key (JSON body: key, value, ttl, dir) |
| `/api/kv/v2/delete` | POST | Yes | Delete a key or directory (JSON body: key, dir) |

### Ops APIs

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/api/ops/defragment` | POST | Yes | Defragment a member (JSON body: member_id) |
| `/api/ops/snapshot` | GET | Yes | Download snapshot from a member (query: member_id) |
| `/api/ops/alarms` | GET | Yes | List active cluster alarms |
| `/api/ops/alarms/disarm` | POST | Yes | Disarm a specific alarm (JSON body: member_id, alarm_type) |
| `/api/ops/move-leader` | POST | Yes | Move leader to target member (JSON body: target_member_id) |
| `/api/ops/hashkv` | POST | Yes | Run HashKV consistency check across all members |
| `/api/ops/audit-logs` | GET | Yes | Query audit logs (query: page, page_size, operation) |

> **Auth**: When etcd auth is enabled, protected endpoints require a valid session (via `Authorization: Bearer <token>` header). When etcd auth is not enabled, all endpoints are open.

## Requirements

| Component | Requirement |
|---|---|
| OS | Linux x86_64 (CentOS 7/8, RHEL, Ubuntu, etc.) |
| etcd | 3.4.x (verified on 3.4.18) |
| Runtime | **None** - statically linked binary, no Go/Node/Python/etcdctl needed |
| Disk | ~250MB (10MB binary + ~200MB for 7 days of data) |

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
