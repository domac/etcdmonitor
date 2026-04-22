<div align="right">

[English](README.md) | **简体中文**

</div>

<div align="center">

# etcdmonitor

**轻量、自包含的 etcd 集群监控面板 —— 单二进制、零依赖。**

<!-- 发布 / 参考 -->
[![Release](https://img.shields.io/github/v/release/domac/etcdmonitor?display_name=tag&sort=semver)](https://github.com/domac/etcdmonitor/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/domac/etcdmonitor.svg)](https://pkg.go.dev/github.com/domac/etcdmonitor)
[![Downloads](https://img.shields.io/github/downloads/domac/etcdmonitor/total)](https://github.com/domac/etcdmonitor/releases)

<!-- 质量 -->
[![Go Report Card](https://goreportcard.com/badge/github.com/domac/etcdmonitor)](https://goreportcard.com/report/github.com/domac/etcdmonitor)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

<!-- 社区 -->
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![Last Commit](https://img.shields.io/github/last-commit/domac/etcdmonitor)](https://github.com/domac/etcdmonitor/commits/master)

<!-- 标签 -->
[![Go](https://img.shields.io/badge/Go-1.21%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![etcd](https://img.shields.io/badge/etcd-3.4.x-419EDA?logo=etcd&logoColor=white)](https://etcd.io)
[![Platform](https://img.shields.io/badge/Platform-Linux%20x86__64-lightgrey)](https://github.com/domac/etcdmonitor/releases)

![etcdmonitor Dashboard](./dashboard.png)

<!-- TODO: 补拍 assets/screenshots/kv-tree.png、ops.png、login.png、themes.png -->

</div>

---

> **安全提示** —— 在生产部署前，请阅读 **[SECURITY.md](./SECURITY.md)** 并完成
> **[docs/SECURITY_CHECKLIST.md](./docs/SECURITY_CHECKLIST.md)** 所列清单。
> 切勿在多台机器间复用示例 TLS 证书 —— 每台目标机器都应使用
> `./tools/gen-certs.sh` 本地生成。

---

## 目录

- [为什么选 etcdmonitor](#为什么选-etcdmonitor)
- [功能亮点](#功能亮点)
- [界面截图](#界面截图)
- [快速开始](#快速开始)
- [从源码构建](#从源码构建)
- [配置](#配置)
- [文档](#文档)
- [架构](#架构)
- [路线图](#路线图)
- [社区与支持](#社区与支持)
- [贡献](#贡献)
- [致谢](#致谢)
- [许可](#许可)

## 为什么选 etcdmonitor

etcdmonitor 是 **一个二进制**，就能提供生产可用的 Dashboard、KV 浏览/编辑器和集群运维面板 —— 无需部署 Prometheus、Grafana 或额外的 exporter。

|  | **etcdmonitor** | Grafana + Prometheus + etcd-exporter | etcdkeeper |
|---|---|---|---|
| 部署形态 | 单二进制 | 3+ 组件 | 单二进制 |
| 开箱指标 | 80+ | 100+（需自配 Dashboard） | — |
| KV 管理（v2 + v3） | ✅ | ❌ | ✅ |
| 运维操作（Defrag / Snapshot / Compact / HashKV / Move Leader） | ✅ | 仅告警 | ❌ |
| 审计日志 | ✅ 内建 | 需额外配置 | ❌ |
| 本地管理员登录 | ✅ | Grafana 自带 | ❌ |
| 暗色 / 亮色主题 | ✅ | ✅ | ❌ |
| 最佳场景 | 中小集群一体化运维 | 多集群大规模指标聚合 | 仅浏览 KV |

**何时选 etcdmonitor**：你想用一个二进制回答"我的 etcd 健康吗、KV 里有什么、能不能做日常运维"，而不想搭建一整套可观测性栈。

**何时选别的**：你已经在大规模运行 Prometheus 并需要跨数十个集群集中聚合指标 —— 把 etcd-exporter 接入现有 Grafana 即可。

## 功能亮点

- **零依赖** —— 单个静态二进制（约 31 MB），内嵌 Web UI、SQLite 存储与一切。
- **多成员集群** —— 通过官方 Go SDK 自动发现成员并并发采集。
- **多端点故障转移** —— 逗号分隔多端点，全局健康管理，自动恢复。
- **KV 树管理** —— 树/列表视图浏览、CRUD、搜索；同时支持 etcd v3 和 v2。
- **运维面板** —— Defragment、Snapshot、Alarms、Move Leader、HashKV、Compact、带 CSV 导出的审计日志。
- **80+ 指标、25 图表** —— 覆盖 Raft、磁盘 I/O、MVCC、Lease、网络、gRPC、Go 运行时。
- **本地管理员登录** —— 与 etcd auth 解耦；bcrypt、锁定、首登强制改密。
- **面板配置** —— 每用户独立的显/隐与拖拽排序。
- **暗色 / 亮色主题** —— 一键切换，偏好保存在浏览器。
- **HTTPS 与 etcd mTLS** —— Dashboard 可选 TLS，etcd 支持 mTLS 客户端证书。
- **自动降采样** —— 智能聚合即使 7 天数据也秒级响应。
- **一键部署** —— `install.sh` 写入硬化后的 systemd 单元，`uninstall.sh` 一键清理。

## 界面截图

| | |
|---|---|
| ![Dashboard](./dashboard.png) | <!-- TODO: 补 assets/screenshots/kv-tree.png --> KV 树 *(截图待补)* |
| <!-- TODO: 补 assets/screenshots/ops.png --> 运维面板 *(截图待补)* | <!-- TODO: 补 assets/screenshots/login.png --> 登录与主题 *(截图待补)* |

## 快速开始

```bash
# 将发布包上传到服务器
unzip etcdmonitor-v<VERSION>-linux-amd64.zip
cd etcdmonitor-v<VERSION>-linux-amd64

# （可选）为 HTTPS 生成本地 TLS 证书
./tools/gen-certs.sh --host monitor.corp.local --ip <server-ip>

# 编辑配置
vim config.yaml

# 安装并启动（默认以 root 运行；生产建议见下）
sudo ./install.sh

# 生产推荐：使用专用非 root 用户
# sudo useradd -r -s /sbin/nologin -d "$(pwd)" etcdmonitor
# sudo ./install.sh --run-user etcdmonitor
```

在浏览器打开 `http://<server-ip>:9090`（启用 TLS 时为 `https://…`）。

**首次启动**时，etcdmonitor 自动创建默认 `admin` 账号。随机生成的初始密码写入
`data/initial-admin-password`（权限 0600），登录页会提示文件路径。首次登录后会
强制修改密码，修改成功后该文件会自动删除。

> 正式对外前请过一遍 [安全检查清单](./docs/SECURITY_CHECKLIST.md)。

## 从源码构建

```bash
git clone https://github.com/domac/etcdmonitor.git
cd etcdmonitor

# 仅构建二进制（用于开发/测试）
./build.sh

# 打出完整部署包（二进制 + 配置 + 安装脚本）
./package.sh
# 输出：dist/etcdmonitor-v<version>-linux-amd64.zip
```

**要求：** Go 1.21+。

## 配置

起步用的最小 `config.yaml`：

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"   # 逗号分隔支持多端点故障转移
server:
  listen: ":9090"
collector:
  interval: 30
storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7
```

完整参考（每个选项、每个默认、每个 TLS 字段）见
**[docs/CONFIGURATION_CN.md](./docs/CONFIGURATION_CN.md)**。

## 文档

| 主题 | 文档 |
|---|---|
| 全部配置选项 | [docs/CONFIGURATION_CN.md](./docs/CONFIGURATION_CN.md) |
| HTTP API 参考 | [docs/API_CN.md](./docs/API_CN.md) |
| HTTPS 与 etcd mTLS | [docs/TLS_CN.md](./docs/TLS_CN.md) |
| 数据保留与存储 | [docs/DATA_RETENTION_CN.md](./docs/DATA_RETENTION_CN.md) |
| 上线前安全检查清单 | [docs/SECURITY_CHECKLIST.md](./docs/SECURITY_CHECKLIST.md) |
| 安全策略与报告渠道 | [SECURITY.md](./SECURITY.md) |
| 贡献指南 | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| 行为准则 | [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) |
| 变更日志 | [CHANGELOG.md](./CHANGELOG.md) |

## 架构

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

## 路线图

- **近期** —— 见 [CHANGELOG.md](./CHANGELOG.md) 的 `[Unreleased]` 段。
- **长期** —— 在 [GitHub Issues](https://github.com/domac/etcdmonitor/issues) 打 `enhancement` 标签投票或提议。
- **版本发布** —— [GitHub Releases](https://github.com/domac/etcdmonitor/releases) 使用 semver 标签。

## 社区与支持

- **缺陷与特性请求** → [GitHub Issues](https://github.com/domac/etcdmonitor/issues)
- **提问与讨论** → [GitHub Discussions](https://github.com/domac/etcdmonitor/discussions)
- **安全问题（私下）** → [SECURITY.md](./SECURITY.md)（**请勿** 在公开 Issue 提交）
- **参与贡献** → [CONTRIBUTING.md](./CONTRIBUTING.md)
- **行为准则** → [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md)

## 贡献

欢迎 PR！完整流程（环境、提交规范、vendor 依赖、测试）见
**[CONTRIBUTING.md](./CONTRIBUTING.md)**。简易路径：

1. Fork 仓库并创建特性分支：`git checkout -b feature/amazing`
2. 安装 pre-commit 钩子（运行 `gofmt`、`go vet`、`gitleaks`）：
   ```bash
   ln -sf ../../tools/pre-commit.sh .git/hooks/pre-commit
   chmod +x .git/hooks/pre-commit
   ```
3. 提交变更，必要时补测试，并在 `CHANGELOG.md` 的 `[Unreleased]` 段记录
4. 推送并基于模板发起 Pull Request

> **安全问题** 必须按 [SECURITY.md](./SECURITY.md) 私下上报 —— 切勿在公开 Issue 中发布。

## 致谢

etcdmonitor 站在众多优秀开源项目的肩膀上：

- **[etcd](https://etcd.io)** —— 本 Dashboard 监控的分布式键值存储
- **[ACE Editor](https://ace.c9.io)** —— 驱动 KV 值编辑视图的浏览器内代码编辑器
- **[Apache ECharts](https://echarts.apache.org)** —— 所有图表的背后
- **[go-sqlite3](https://github.com/mattn/go-sqlite3)** —— Go 的零依赖 SQLite 绑定
- **[Contributor Covenant](https://www.contributor-covenant.org)** —— 我们采用的行为准则

## 许可

本项目遵循 MIT 许可 —— 完整文本见 [LICENSE](LICENSE)。

<!-- 更新此文件时，请同步更新 README.md：结构、徽章、截图必须保持一致。 -->
