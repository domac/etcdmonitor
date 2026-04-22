<div align="right">

**English** | [简体中文](README_CN.md)

</div>

<div align="center">

# etcdmonitor

**Lightweight, self-contained monitoring dashboard for etcd clusters — single binary, zero dependencies.**

<!-- Release / Reference -->
[![Release](https://img.shields.io/github/v/release/domac/etcdmonitor?display_name=tag&sort=semver)](https://github.com/domac/etcdmonitor/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/domac/etcdmonitor.svg)](https://pkg.go.dev/github.com/domac/etcdmonitor)
[![Downloads](https://img.shields.io/github/downloads/domac/etcdmonitor/total)](https://github.com/domac/etcdmonitor/releases)

<!-- Quality -->
[![Go Report Card](https://goreportcard.com/badge/github.com/domac/etcdmonitor)](https://goreportcard.com/report/github.com/domac/etcdmonitor)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

<!-- Community -->
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![Last Commit](https://img.shields.io/github/last-commit/domac/etcdmonitor)](https://github.com/domac/etcdmonitor/commits/master)

<!-- Tags -->
[![Go](https://img.shields.io/badge/Go-1.21%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![etcd](https://img.shields.io/badge/etcd-3.4.x-419EDA?logo=etcd&logoColor=white)](https://etcd.io)
[![Platform](https://img.shields.io/badge/Platform-Linux%20x86__64-lightgrey)](https://github.com/domac/etcdmonitor/releases)

![etcdmonitor Dashboard](./dashboard.png)

<!-- TODO: add assets/screenshots/kv-tree.png, ops.png, login.png, themes.png -->

</div>

---

> **Security notice** — Before deploying to production, read
> **[SECURITY.md](./SECURITY.md)** and work through
> **[docs/SECURITY_CHECKLIST.md](./docs/SECURITY_CHECKLIST.md)**.
> Never reuse example TLS certificates across machines — generate a
> local key on every deployment target with `./tools/gen-certs.sh`.

---

## Table of Contents

- [Why etcdmonitor](#why-etcdmonitor)
- [Features](#features)
- [Screenshots](#screenshots)
- [Quick Start](#quick-start)
- [Build from Source](#build-from-source)
- [Configuration](#configuration)
- [Documentation](#documentation)
- [Architecture](#architecture)
- [Roadmap](#roadmap)
- [Community & Support](#community--support)
- [Contributing](#contributing)
- [Acknowledgments](#acknowledgments)
- [License](#license)

## Why etcdmonitor

etcdmonitor is **one binary** that gives you a production-grade
Dashboard, a KV browser/editor, and a cluster-ops panel — without
deploying Prometheus, Grafana, or an extra exporter.

|  | **etcdmonitor** | Grafana + Prometheus + etcd-exporter |
|---|---|---|
| Deployment | Single binary | 3+ components |
| Metrics coverage | 80+ out of the box | 100+ (requires dashboards) |
| KV management (v2 + v3) | ✅ | ❌ |
| Ops actions (Defrag / Snapshot / Compact / HashKV / Move Leader) | ✅ | Alerting only |
| Audit log | ✅ built-in | Requires extra setup |
| Local admin login | ✅ | Grafana built-in |
| Dark / Light theme | ✅ | ✅ |
| Best-fit scenario | Small/mid clusters, all-in-one ops | Large-scale multi-cluster aggregation |

**Use etcdmonitor when** you want a single binary to answer "is my etcd
healthy, what's inside its KV space, and can I run maintenance on it?"
without standing up a full observability stack.

**Look elsewhere when** you already run Prometheus at scale and need
centralized metrics across dozens of clusters — federate etcd-exporter
into your existing Grafana instead.

## Features

- **Zero dependencies** — Single static binary (~31 MB) embeds web UI, SQLite storage, everything.
- **Multi-member cluster** — Auto-discovers members via official Go SDK with concurrent collection.
- **Multi-endpoint failover** — Comma-separated endpoints with global health management and auto-recovery.
- **KV Tree management** — Browse, CRUD, and search keys in a tree/list view, supports etcd v3 & v2.
- **Ops panel** — Defragment, Snapshot, Alarms, Move Leader, HashKV, Compact, Audit Log with CSV export.
- **80+ metrics, 25 charts** — Raft, disk I/O, MVCC, Lease, network, gRPC, Go runtime.
- **Local admin login** — Independent of etcd auth; bcrypt, lockout, forced first-time password change.
- **Panel configuration** — Per-user show/hide & drag-to-reorder for monitoring panels.
- **Dark / Light theme** — One-click toggle, preference saved in browser.
- **HTTPS & etcd mTLS** — Optional TLS for Dashboard, mTLS client certs for etcd.
- **Auto downsampling** — Smart query aggregation stays responsive with 7 days of data.
- **One-click deploy** — `install.sh` writes a hardened systemd unit, `uninstall.sh` cleans up.

## Screenshots

| | |
|---|---|
| ![Dashboard](./dashboard.png) | <!-- TODO: add assets/screenshots/kv-tree.png --> KV Tree *(screenshot coming soon)* |
| <!-- TODO: add assets/screenshots/ops.png --> Ops Panel *(screenshot coming soon)* | <!-- TODO: add assets/screenshots/login.png --> Login & Themes *(screenshot coming soon)* |

## Quick Start

```bash
# Upload the release zip to your server
unzip etcdmonitor-v<VERSION>-linux-amd64.zip
cd etcdmonitor-v<VERSION>-linux-amd64

# (Optional) Generate a local TLS certificate for HTTPS
./tools/gen-certs.sh --host monitor.corp.local --ip <server-ip>

# Edit config
vim config.yaml

# Install & start (default: runs as root; see below for production)
sudo ./install.sh

# RECOMMENDED for production: dedicated non-root user
# sudo useradd -r -s /sbin/nologin -d "$(pwd)" etcdmonitor
# sudo ./install.sh --run-user etcdmonitor
```

Open `http://<server-ip>:9090` (or `https://…` with TLS) in your browser.

On **first startup**, etcdmonitor auto-creates a default `admin` account.
The randomly-generated initial password is written to
`data/initial-admin-password` (mode 0600); the login page tells you which
file to read. You are forced to change the password on first login, after
which the file is deleted automatically.

> Before exposing to production, walk through the
> [Security checklist](./docs/SECURITY_CHECKLIST.md).

## Build from Source

```bash
git clone https://github.com/domac/etcdmonitor.git
cd etcdmonitor

# Build binary only (for development/testing)
./build.sh

# Create full deployment package (binary + config + install scripts)
./package.sh
# Output: dist/etcdmonitor-v<version>-linux-amd64.zip
```

**Requirements:** Go 1.21+.

## Configuration

Minimum `config.yaml` to get started:

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"   # comma-separated for multi-endpoint failover
server:
  listen: ":9090"
collector:
  interval: 30
storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7
```

Full reference (every option, every default, every TLS field) →
**[docs/CONFIGURATION.md](./docs/CONFIGURATION.md)**.

## Documentation

| Topic | Document |
|---|---|
| All configuration options | [docs/CONFIGURATION.md](./docs/CONFIGURATION.md) |
| HTTP API reference | [docs/API.md](./docs/API.md) |
| HTTPS & etcd mTLS | [docs/TLS.md](./docs/TLS.md) |
| Data retention & storage | [docs/DATA_RETENTION.md](./docs/DATA_RETENTION.md) |
| Pre-production security checklist | [docs/SECURITY_CHECKLIST.md](./docs/SECURITY_CHECKLIST.md) |
| Security policy & reporting | [SECURITY.md](./SECURITY.md) |
| Contributing guide | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| Code of conduct | [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) |
| Change log | [CHANGELOG.md](./CHANGELOG.md) |

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                  etcdmonitor (single binary)                   │
│                                                                │
│  ┌───────────┐   ┌──────────┐   ┌────────────────────────┐   │
│  │ Collector  │──▶│  SQLite  │◀──│  HTTP/S API            │   │
│  │ (30s poll) │   │ (per     │   │  /api/current          │   │
│  │ concurrent │   │  member) │   │  /api/range            │   │
│  └─────┬──┬──┘   └──────────┘   │  /api/members          │   │
│        │  │                      │  /api/status           │   │
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

## Roadmap

- **Near term** — tracked in `[Unreleased]` of [CHANGELOG.md](./CHANGELOG.md).
- **Longer term** — file ideas / vote via [GitHub Issues](https://github.com/domac/etcdmonitor/issues) with the `enhancement` label.
- **Releases** — [GitHub Releases](https://github.com/domac/etcdmonitor/releases) with semver tags.

## Community & Support

- **Bug reports & feature requests** → [GitHub Issues](https://github.com/domac/etcdmonitor/issues)
- **Questions & ideas** → [GitHub Discussions](https://github.com/domac/etcdmonitor/discussions)
- **Security reports (privately)** → [SECURITY.md](./SECURITY.md) (do **not** file public issues)
- **Contributing** → [CONTRIBUTING.md](./CONTRIBUTING.md)
- **Code of conduct** → [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md)

## Contributing

Pull requests are welcome! The full guide (environment, commit
conventions, vendor deps, testing) lives in
**[CONTRIBUTING.md](./CONTRIBUTING.md)**. Quick path:

1. Fork the repo and create a feature branch: `git checkout -b feature/amazing`
2. Install the pre-commit hook (runs `gofmt`, `go vet`, `gitleaks`):
   ```bash
   ln -sf ../../tools/pre-commit.sh .git/hooks/pre-commit
   chmod +x .git/hooks/pre-commit
   ```
3. Make your change, add tests where it makes sense, update `CHANGELOG.md` under `[Unreleased]`
4. Commit and push; open a Pull Request using the provided template

> **Security issues** must be reported privately per
> [SECURITY.md](./SECURITY.md) — never in public issues.

## Acknowledgments

etcdmonitor stands on the shoulders of great open-source projects:

- **[etcd](https://etcd.io)** — the distributed key-value store this dashboard monitors
- **[ACE Editor](https://ace.c9.io)** — in-browser code editor powering the KV value view
- **[Apache ECharts](https://echarts.apache.org)** — the charting library behind every panel
- **[go-sqlite3](https://github.com/mattn/go-sqlite3)** — zero-dependency SQLite binding for Go
- **[Contributor Covenant](https://www.contributor-covenant.org)** — the Code of Conduct we adopt

## License

Released under the MIT License — see [LICENSE](LICENSE) for the full text.

<!-- When updating this file, also sync README_CN.md: structure, badges, and screenshots must stay in lockstep. -->
