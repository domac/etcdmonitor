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

- **Zero dependencies** - Single static binary (~10MB), embeds web UI, SQLite storage, everything
- **Multi-member cluster** - Auto-discovers all etcd members via `etcdctl`, concurrent metrics collection
- **50+ metrics, 18 charts** - Covers Raft, disk I/O, MVCC, network, gRPC, Go runtime
- **Dark / Light theme** - Toggle with one click, preference saved in browser
- **HTTPS support** - Optional TLS with bundled self-signed certificates, or bring your own
- **Auto downsampling** - Smart query aggregation keeps dashboard responsive even with 7 days of data
- **One-click deploy** - `install.sh` sets up systemd service, `uninstall.sh` cleans up
- **Smart endpoint detection** - Supports `127.0.0.1` config, auto-resolves to real member ID
- **Data isolation** - Per-member storage in SQLite, auto-cleanup on cluster endpoint change
- **Data retention** - Configurable retention period (default 7 days), automatic expired data purge with disk reclaim

## Quick Start

### Download & Deploy

```bash
# Upload to server

unzip etcdmonitor-v0.2.0-linux-amd64.zip
cd etcdmonitor-v0.2.0-linux-amd64

# Edit config
vim config.yaml

# Install & start
sudo ./install.sh
```

Open `http://<server-ip>:9090` in your browser.

### Build from Source

```bash
git clone https://github.com/<your-username>/etcdmonitor.git
cd etcdmonitor

# Build for Linux
./build.sh

# Create distributable package
./package.sh
# Output: dist/etcdmonitor-v<version>-linux-amd64.zip
```

**Requirements:** Go 1.21+

## Configuration

Edit `config.yaml`:

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"
  username: "root"
  password: "your-password"
  metrics_path: "/metrics"
  auth_enable: false                    # true: gRPC-gateway API, false: etcdctl
  bin_path: "/data/services/etcd/bin"   # etcdctl path (when auth_enable: false)

server:
  listen: ":9090"
  tls_enable: false                     # true: HTTPS, false: HTTP
  tls_cert: "certs/server.crt"
  tls_key: "certs/server.key"

collector:
  interval: 30          # seconds

storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7

log:
  dir: "logs"
  max_size_mb: 50
  max_files: 5
```

| Parameter | Description | Default |
|---|---|---|
| `etcd.endpoint` | etcd client endpoint | `http://127.0.0.1:2379` |
| `etcd.username` | Auth username (leave empty if no auth) | - |
| `etcd.password` | Auth password | - |
| `etcd.auth_enable` | Member discovery: `true` = v3 HTTP API, `false` = etcdctl | `true` |
| `etcd.bin_path` | etcdctl binary directory (when `auth_enable: false`) | `/data/services/etcd/bin` |
| `server.listen` | Dashboard listen address | `:9090` |
| `server.tls_enable` | Enable HTTPS | `false` |
| `server.tls_cert` | TLS certificate file path | `certs/server.crt` |
| `server.tls_key` | TLS private key file path | `certs/server.key` |
| `collector.interval` | Metrics collection interval (seconds) | `30` |
| `storage.retention_days` | Data retention period (days) | `7` |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                 etcdmonitor (single binary)           │
│                                                       │
│  ┌───────────┐   ┌──────────┐   ┌─────────────────┐ │
│  │ Collector  │──▶│  SQLite  │◀──│  HTTP/S API     │ │
│  │ (30s poll) │   │ (per     │   │  /api/current   │ │
│  │ concurrent │   │  member)  │   │  /api/range     │ │
│  └─────┬──┬──┘   └──────────┘   │  /api/members   │ │
│        │  │                      │  /api/status     │ │
│        │  │                      └────────┬────────┘ │
│        │  │                               │          │
│    ┌───▼──▼───┐                  ┌────────▼────────┐ │
│    │  etcd    │                  │  Dashboard      │ │
│    │ /metrics │                  │  (ECharts)      │ │
│    │ x N nodes│                  │  Dark / Light   │ │
│    └──────────┘                  └─────────────────┘ │
└─────────────────────────────────────────────────────┘
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

### Chart Panels (18 charts)

| Section | Charts | Key Metrics |
|---|---|---|
| **Raft & Server** | Proposals, Leader Changes, Commit-Apply Lag, Failed Rate | `proposals_*`, `leader_changes`, `slow_apply` |
| **Disk Performance** | WAL Fsync Duration, Backend Commit Duration | P50/P90/P99 latency histograms |
| **MVCC & Storage** | Database Size, MVCC Operations | `db_total_size`, `put/delete/txn/range` totals |
| **Network & Peers** | Peer Traffic, Peer RTT | `peer_sent/received_bytes`, RTT P50/P90/P99 |
| **gRPC Requests** | Request Rate, Client Traffic | `grpc_server_handled` (OK/Error), traffic bytes |
| **Process & Runtime** | CPU Usage, Memory, Goroutines, GC Duration, File Descriptors, Memory Sys | CPU %, RSS, heap, GC pause, FDs, sys memory |

## HTTPS / TLS

The install package includes a self-signed TLS certificate (valid for 10 years). To enable HTTPS:

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
./gen-certs.sh          # default: 10 years
./gen-certs.sh 365      # custom: 1 year
```

> Self-signed certificates will trigger a browser warning. Click "Advanced" > "Proceed" to continue, or use a certificate from a trusted CA for production.

## Multi-Member Cluster Support

etcd Monitor automatically discovers all cluster members and collects metrics concurrently.

**Member discovery modes:**

| Mode | Config | How it works | When to use |
|---|---|---|---|
| **etcdctl** | `auth_enable: false` | Runs `etcdctl member list -w json` | etcd without gRPC-gateway (most 3.4 setups) |
| **v3 HTTP API** | `auth_enable: true` | `POST /v3/cluster/member/list` | etcd with gRPC-gateway enabled |

- Members are refreshed every 60 seconds (handles scaling events)
- Each member's metrics are stored with its own `member_id` in SQLite
- Dashboard header has a dropdown to switch between members
- Local node is auto-detected even when config uses `127.0.0.1`

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

The service runs as a non-root user when available (configurable in `install.sh`), with auto-restart on crash.

## Endpoint Change Detection

When `etcd.endpoint` in `config.yaml` changes (pointing to a different cluster), all historical data is automatically purged on restart to prevent data mixing. Switching between members in the dashboard does **not** trigger data cleanup.

## Uninstall

```bash
sudo ./uninstall.sh
```

Removes the systemd service. Optionally deletes data and logs (interactive prompt). Binary and config are preserved for re-installation.

## Project Structure

```
etcdmonitor/
├── cmd/etcdmonitor/
│   └── main.go                 # Entry point, HTTP/HTTPS server, logger
├── internal/
│   ├── config/config.go        # Configuration loading & defaults
│   ├── collector/
│   │   ├── collector.go        # Concurrent metrics collection engine
│   │   ├── member.go           # Cluster member discovery (etcdctl / API)
│   │   └── parser.go           # Prometheus text format parser
│   ├── storage/storage.go      # SQLite time-series storage with downsampling
│   └── api/
│       ├── api.go              # Router & middleware
│       └── handler.go          # REST API handlers
├── web/                        # Embedded frontend (HTML/CSS/JS + ECharts)
├── certs/                      # TLS certificates (generated, git-ignored)
├── embed.go                    # go:embed for web assets
├── config.yaml                 # Default configuration
├── gen-certs.sh                # TLS certificate generator
├── build.sh                    # Build script
├── package.sh                  # Package script (build + zip)
├── install.sh                  # One-click install (systemd)
├── uninstall.sh                # One-click uninstall
└── version                     # Version file (read by build scripts)
```

## API Reference

| Endpoint | Method | Description |
|---|---|---|
| `/api/members` | GET | List all cluster members |
| `/api/current?member_id=<id>` | GET | Latest metrics snapshot for a member |
| `/api/range?member_id=<id>&metrics=m1,m2&range=1h` | GET | Time-series data for specified metrics |
| `/api/status` | GET | Monitor system status & cluster info |
| `/api/debug` | GET | Debug info: DB member IDs, collector state |

## Requirements

| Component | Requirement |
|---|---|
| OS | Linux x86_64 (CentOS 7/8, RHEL, Ubuntu, etc.) |
| etcd | 3.4.x (verified on 3.4.18) |
| Runtime | **None** - statically linked binary, no Go/Node/Python needed |
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
