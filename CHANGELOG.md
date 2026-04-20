# Changelog

All notable changes to this project will be documented in this file. The
format is based on [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- **BREAKING**: Revoke and remove the example self-signed TLS certificate that
  was previously committed at `certs/server.key` / `certs/server.crt`. The
  key had `CN=etcdmonitor` and no CA trust, but it is still considered
  compromised because the repository was public. All deployments must
  regenerate a local key via `./tools/gen-certs.sh` on the target machine.
  `install.sh` refuses to start when `tls_enable: true` and the key files are
  absent. See the Historical Advisories section in `SECURITY.md`.
- Harden `Content-Security-Policy`: remove `'unsafe-eval'`, remove
  `https://cdn.jsdelivr.net` from `script-src`; explicitly declare
  `worker-src 'self' blob:` so that the ACE editor syntax worker in the KV
  panel keeps functioning under the stricter policy. `script-src 'unsafe-inline'`
  is retained for now as a known limitation — the login / change-password pages
  and ~59 inline `on*` handlers in `index.html` still depend on it. Tightening
  this further (extract scripts, migrate to `addEventListener`, adopt nonces)
  is tracked as a follow-up CSP v2 change.
- Vendor echarts 5.5.0 into `web/vendor/echarts/` and embed it into the binary
  via the existing `go:embed web/*`. The dashboard no longer loads any script
  from the public internet — it works fully offline and the jsdelivr CDN
  supply-chain attack surface is removed.
- Escape backend / user-provided error messages before inserting them into
  `innerHTML` in `web/ops.js` (9 call sites) via a new `web/util.js`
  `escapeHTML()` helper.
- `install.sh` now prints a WARN (to terminal and journal) when the service is
  installed to run as `root`, and exposes `--run-user <name>` to switch to a
  dedicated non-root account. Default run user remains `root` for upgrade
  continuity; see `docs/SECURITY_CHECKLIST.md` for the recommended production
  setup.

### Added

- `SECURITY.md`: vulnerability disclosure policy, supported versions, response
  SLA, and a historical advisory for the revoked example certificate.
- `CONTRIBUTING.md`: development environment, commit conventions, pre-commit
  hook installation, vendor dependency management.
- `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1).
- `.github/workflows/ci.yml`: three parallel jobs — build-test (with race
  detector), lint (`gofmt`, `go vet`), security-scan (`govulncheck`,
  `gitleaks`, `trivy fs`).
- `.github/ISSUE_TEMPLATE/` (bug / feature) and `PULL_REQUEST_TEMPLATE.md`.
- `docs/SECURITY_CHECKLIST.md`: 7-group production readiness checklist for
  systemd deployments.
- `tools/pre-commit.sh`: local git hook running `gofmt`, `go vet`, and
  `gitleaks protect --staged` (if installed).
- `tools/vendor-echarts.sh`: reproducible download + SHA256 verification for
  vendored echarts bundles. `web/vendor/echarts/VERSION` pins the exact
  version; `SHA256SUMS` records the upstream URL + hash.
- `.gitleaks.toml`: project-specific allowlist for known test fixtures.
- `tools/gen-certs.sh` now accepts `--host <hostname>`, `--ip <address>` (both
  repeatable), `--days <N>`, and `--force`. This lets operators generate
  certificates with SANs that actually match their access hostnames without
  hand-editing the script.

### Changed

- `install.sh`:
  - Adds `--run-user <name>` to explicitly run the service as a dedicated
    non-root user. Default remains `root` for upgrade continuity; running as
    root now emits a WARN to terminal and journal pointing operators to the
    recommended configuration.
  - systemd unit gains a widely-supported sandbox: `NoNewPrivileges`,
    `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`,
    `RestrictSUIDSGID`, `RestrictNamespaces`, `LockPersonality`,
    `MemoryDenyWriteExecute`, `SystemCallArchitectures=native`,
    empty `CapabilityBoundingSet` / `AmbientCapabilities`, explicit
    `ReadWritePaths=$INSTALL_DIR/data $INSTALL_DIR/logs`, `UMask=0077`.
  - Pre-flight check: if `config.yaml` has `tls_enable: true` but
    `certs/server.{key,crt}` are missing, abort with a clear message pointing
    to `./tools/gen-certs.sh`.
  - Respects user-provided certificate symlinks — skips `chmod` on targets
    that are symbolic links (so linking to a central cert management directory
    does not inadvertently change its permissions).
  - Closing banner recommends `systemd-analyze security etcdmonitor` for
    verification.
- `package.sh` no longer bundles a generated TLS key/cert into the release zip.
  It now ships `certs/README.md` and the `tools/gen-certs.sh` script so
  operators must generate certificates on the target host. A defensive check
  aborts packaging if `.key` / `.crt` / `.pem` / `.p12` / `.pfx` files slip
  into the archive.
- `.gitignore` hardens `certs/*.key`, `certs/*.crt`, `certs/*.pem`,
  `certs/*.p12`, `certs/*.pfx` (previously commented out and therefore
  inactive). `certs/.gitkeep` preserves the directory.

### Removed

- **BREAKING**: `certs/server.key`, `certs/server.crt` removed from the
  repository (see Security → Revoke section).
- `package.sh` no longer generates or ships a `CN=etcdmonitor` self-signed
  key at package time.

## [0.8.0] - 2026-04-20

### Added

- **本地用户认证体系**（`local-user-auth`）：Dashboard 登录从"跟随 etcd auth"改为独立的本地账号体系
  - 首次启动自动生成随机 16 字符初始密码（`crypto/rand` + base62，排除视觉歧义字符 `0/O/o/1/l/I`），bcrypt 哈希写入新 `users` 表
  - 初始密码明文写入 `data/initial-admin-password`（权限 `0600`），启动日志打印文件路径而非密码
  - 首次登录后强制修改密码（`must_change_password=1`），修改成功后系统自动删除该文件
  - 登录页在 `initial_setup_pending=true` 时显示小字提示"初始密码保存在 data/initial-admin-password 文件中"（不泄露任何具体密码字面量）
- **修改密码接口**：`POST /api/auth/change-password`（零 token 设计，凭 `username + old_password` 授权），前端新增 `web/change-password.html` 改密页
- **登录失败锁定**：同一账号连续 5 次失败（`login` 与 `change-password` **共享计数**）锁定 15 分钟，状态落盘到 `users` 表，进程重启保留
- **CLI 子命令**：
  - `etcdmonitor reset-password --username admin`：交互式重置密码，自动置 `must_change_password=1` + 清锁
  - `etcdmonitor unlock --username admin`：仅清锁，不改密码
- **审计日志覆盖认证事件**：`login` / `login_lockout` / `change_password` / `cli_reset_password` / `cli_unlock` / `initial_password_file_deleted` 全部入库
- `users` 表保留 `role` 字段（默认 `admin`），为后续 RBAC 预留 schema
- 新增 `auth:` 配置段（可选）：`bcrypt_cost` / `lockout_threshold` / `lockout_duration_seconds` / `min_password_length`
- **空表兜底恢复**：管理员清空 `users` 表后重启，系统自动重新生成新 admin 与新初始密码文件

### Changed

- **BREAKING**：Dashboard 无条件要求本地登录，彻底移除"etcd auth 未启用 → 免登录直接访问"的旁路；`authMiddleware` 不再根据 etcd auth 状态放行任何受保护路由
- **BREAKING**：`/api/auth/login` 改为校验本地 `users` 表（bcrypt），不再调用 `clientv3.Authenticate`
- `config.yaml` 中 `etcd.username` / `etcd.password` 语义澄清：**仅供 Collector / KV Manager / Ops 等 SDK 客户端使用**，与 Dashboard 登录完全解耦
- `GET /api/auth/status` 返回语义：`auth_required` 恒为 `true`；新增 `initial_setup_pending` 字段；不再返回 `must_change_password`（仅 login 响应携带）
- `DetectAuthRequired()` 不再决定 Dashboard 访问控制，仅用于告知 SDK 客户端是否需要携带凭据

### Security

- 密码使用 **bcrypt**（默认 cost 10，可配置 8–14）；禁止在代码、日志、前端中出现明文默认密码
- 数据目录权限强制收紧：`data/` 为 `0700`，`data/etcdmonitor.db` 与 `data/initial-admin-password` 为 `0600`（`install.sh` 与运行时双保险）
- 锁定期内即使密码正确也直接拒绝，且不做 bcrypt 比对（避免 timing side-channel 泄露）
- 启动日志、审计日志严禁输出明文密码；CLI 两次交互静默读取（`golang.org/x/term`）

## [0.7.0] - 2026-04-17

### Added

- 运维面板新增 Compact（集群压缩）功能：
- 审计日志筛选器新增 "Compact" 操作类型选项

## [0.6.0] - 2026-04-15

### Added

- 运维面板（Ops Panel）：新增集群运维操作中心，支持 6 项运维功能
  - Defragment：在线碎片整理，自动 Follower 优先排序，逐节点执行
  - Snapshot：集群快照备份，流式下载到浏览器，服务端零临时文件
  - Alarms：查看和解除集群告警（NOSPACE、CORRUPT）
  - Move Leader：Leader 迁移，自动定位 Leader 节点发起调用
  - HashKV Check：跨成员数据一致性校验
  - Audit Log：全操作审计日志，记录用户、时间、操作、结果和耗时
- 审计日志专业表格：支持列排序（点击表头升/降序切换）、分页导航、CSV 导出
- 审计日志 SQLite 存储：后端自动记录所有运维操作，支持分页和按操作类型筛选

### Changed

- Ops 面板采用左右分栏布局（左侧菜单导航 + 右侧操作面板），替代卡片网格入口
- 审计日志设为 Ops 面板默认首屏展示
- 左侧菜单纯文字显示，移除字母图标

### Fixed

- 修复 Move Leader 在非 Leader 节点调用报 "etcdserver: not leader" 的问题，改为通过 Status + MemberList 自动定位 Leader 端点

## [0.5.6] - 2026-04-14

### Changed

- 安装脚本运行用户改为可配置：移除硬编码 `mqq` 用户检测，改为脚本顶部 `RUN_USER` 变量（默认 `root`）
- 安装时校验指定用户是否存在，不存在则报错退出并提示创建命令
- 用户组通过 `id -gn` 自动推导，无需手动指定

## [0.5.5] - 2026-04-13

### Added

- KV Tree 搜索过滤：左侧树面板新增搜索框，实时过滤 key（大小写不敏感子串匹配）
- Keys-only API：新增 `GET /api/kv/v3/keys` 和 `GET /api/kv/v2/keys` 接口

### Changed

- 首次加载树改用 keys-only API，不再拉取 value，大幅降低首屏加载时间和带宽
- 展开目录改为纯本地操作（treeData 已含完整子节点），零网络开销，瞬时响应
- 点击节点时按需调用 `GET /get?key=xxx` 加载最新 value/TTL/revision
- `client_v3.go` 重构：提取 `getTree()` 内部方法，`keysOnly` 参数复用树构建逻辑

### Fixed

- 修复 V3 虚拟目录（路径前缀无实际 key）点击后返回 key_not_found 导致整个子树被误删的问题

## [0.5.0] - 2026-04-13

### Added

- KV Tree 管理模块：支持 etcd v3/v2 双协议的键值浏览、创建、编辑、删除
- 全局健康端点管理器（`internal/health/manager.go`）：
- 多端点配置支持：`config.yaml` 的 `endpoint` 字段支持逗号分隔多地址（如 `http://10.10.10.1:2379,http://10.10.10.2:2379`）
- 监控大盘新增 Version 信息卡片，展示 etcd 集群版本
- 监控面板最大化/最小化切换

### Changed

- 全面使用 etcd 官方 Go SDK（`go.etcd.io/etcd/client/v3`）替代 etcdctl 二进制调用
- Collector 的 `statusFromAnyEndpoint()` 改由健康管理器统一调度，移除 `lastGoodEndpoint` 缓存
- KV Manager 的 `newClient()` 使用健康端点列表创建客户端，避免连接故障节点
- 配置移除 `discovery_via_api`、`bin_path` 字段（不再依赖 etcdctl 二进制）
- `request_timeout` 默认值从 5 秒调整为 30 秒

### Fixed

- 修复第一个端点不可用时切换成员节点卡顿问题
- 修复 Create Node 对话框提交导致页面刷新（button type 默认 submit）
- 修复创建子节点后父目录 TTL 丢失问题
- 修复 V3 虚拟目录点击报 "No keys found" 误删节点问题
- 修复目录 value 编辑后无法保存的问题
- 修复 V3/V2 协议切换后树状态重置问题
- 修复 List 模式下节点 TTL 不随点击刷新问题

## [0.3.3] - 2026-04-12

### Added

- 新增 7 个扩展监控面板（默认隐藏）：
- Collector 新增约 30 项 etcd 指标采集，覆盖 Server/Disk/MVCC/Lease/Network/gRPC 扩展指标
- 新增 `ExtractHistogramMs` 解析器，自动将毫秒单位 Histogram 转换为秒
- 新增 `ExtractClientRequests` 解析器，按 `client_api_version` 标签聚合 client_requests_total

### Changed

- 面板标签（Counter/Gauge/Rate 等）默认隐藏，保持界面简洁
- `DefaultPanels` 扩展至 25 项，新增 7 个扩展面板（Order 18–24，Visible: false）
- `mergeWithDefaults` 升级配置时使用 DefaultPanels 中的默认可见性，而非硬编码 true
- 前端 `PANEL_REGISTRY` 扩展至 25 项，`ALL_RANGE_METRICS` 新增约 50 个指标名

## [0.3.2] - 2026-04-12

### Added

- 监控面板可配置：Dashboard 新增齿轮按钮（⚙），点击弹出面板配置窗口
- 面板可见性选择：勾选/取消勾选控制 18 个监控面板的显示与隐藏，默认全选
- 面板拖拽排序：同分区内支持鼠标拖拽排序，禁止跨分区拖拽
- 保存与重置：配置窗口提供"Save"和"Reset"按钮，重置恢复默认（全选、原始顺序）
- 用户级持久化：面板配置与登录用户绑定，同一用户不同设备/浏览器看到相同配置
- 后端用户偏好存储：`internal/prefs/` 包，JSON 文件存储（`data/user-prefs/<username>.json`）
- 新增 API：`GET /api/user/panel-config`、`PUT /api/user/panel-config`，受认证中间件保护
- 免认证模式降级：未启用 etcd 认证时，面板配置存储在浏览器 localStorage
- 分区自动隐藏：分区内全部面板隐藏时，分区标题自动隐藏

### Changed

- 面板 HTML 结构添加 `data-panel-id` 和 `data-section` 属性，支持动态渲染
- `initAllCharts()` 仅对可见面板初始化 ECharts 实例，隐藏面板自动 dispose 释放资源
- `api.New()` 签名扩展，新增 `prefsStore` 参数注入

## [0.3.0] - 2026-04-12

### Added

- Dashboard 登录认证：etcd 启用认证时，运维人员需通过登录页验证身份才能访问大盘
- 启动时自动检测 etcd 认证状态，零配置，无需手动声明
- 登录页面（`web/login.html`），支持 dark/light 主题
- 会话管理：内存会话 + 可配置超时（`server.session_timeout`，默认 3600 秒）
- API 认证中间件：认证模式下 `/api/*` 接口受保护，未认证返回 401
- Dashboard 登出按钮（仅认证模式显示）
- 新增 API：`POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/auth/status`
- 双轨认证：同时支持 `Authorization: Bearer <token>` 和 Cookie

### Changed

- 配置字段 `auth_enable` 重命名为 `discovery_via_ap`
- Logger 全局函数增加 nil 安全检查
- 前端所有 API 请求统一通过 `fetchWithAuth` 包装，401 自动跳转登录页

### Security

- 会话令牌：`crypto/rand` 生成 256 位随机数
- Cookie 属性：`HttpOnly`、`SameSite=Lax`、TLS 时 `Secure`
- 登录凭据一次性验证，不存储在会话中，密码不记录日志
- 过期会话后台自动清理
