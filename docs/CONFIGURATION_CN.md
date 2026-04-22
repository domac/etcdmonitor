← 返回 [README](../README_CN.md)

# 配置参考

本文档是 etcdmonitor 的 **完整配置参考**。主 [README](../README_CN.md) 仅保留最小片段；本文件是所有选项的权威来源。

## 完整 `config.yaml`

```yaml
etcd:
  endpoint: "http://127.0.0.1:2379"         # 支持多端点（逗号分隔）实现故障转移
  username: ""                              # 采集器凭据（无鉴权时留空）
  password: ""
  metrics_path: "/metrics"
  tls_enable: false                         # true：通过 TLS 连接 etcd
  tls_cert: "certs/client.crt"              # 客户端证书（mTLS）
  tls_key: "certs/client.key"               # 客户端私钥（mTLS）
  tls_ca_cert: "certs/ca.crt"               # CA 证书（验证 etcd 服务端）
  tls_insecure_skip_verify: false           # 跳过服务端证书校验（仅用于测试）
  tls_server_name: ""                       # SNI 校验使用的服务名

server:
  listen: ":9090"
  tls_enable: false                         # true：HTTPS；false：HTTP
  tls_cert: "certs/server.crt"
  tls_key: "certs/server.key"
  session_timeout: 3600                     # Dashboard 会话超时（秒）

collector:
  interval: 30          # 秒

storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7

kv_manager:
  separator: "/"                            # 树形视图的 key 路径分隔符
  connect_timeout: 5                        # etcd 连接超时（秒）
  request_timeout: 30                       # etcd 请求超时（秒）
  max_value_size: 2097152                   # value 最大字节数（默认 2MB）

ops:
  ops_enable: true                          # 启用运维面板；false 时隐藏 Ops 标签并拦截 /api/ops/*
  audit_retention_days: 7                   # 审计日志保留天数，过期自动清理

auth:
  bcrypt_cost: 10                           # 密码哈希 cost（8–14；越界回退到 10 并告警）
  lockout_threshold: 5                      # 连续 N 次失败锁定账号（登录与改密共享计数）
  lockout_duration_seconds: 900             # 锁定时长（秒），默认 15 分钟
  min_password_length: 8                    # 新密码最短长度

log:
  dir: "logs"
  filename: "etcdmonitor.log"
  level: "info"                             # debug、info、warn、error
  max_size_mb: 50                           # 单个日志文件最大 MB
  max_files: 5                              # 保留的日志文件数
  max_age: 30                               # 最长保留天数（0 = 不限）
  compress: false                           # 轮转文件是否 gzip 压缩
  console: true                             # 是否同时输出到控制台
```

## 参数说明

| 参数 | 说明 | 默认 |
|---|---|---|
| `etcd.endpoint` | etcd 客户端端点（逗号分隔支持多端点故障转移） | `http://127.0.0.1:2379` |
| `etcd.username` | 采集器用户名（无鉴权留空） | — |
| `etcd.password` | 采集器密码 | — |
| `etcd.tls_enable` | 启用 etcd 客户端 TLS | `false` |
| `etcd.tls_cert` | 客户端证书文件路径（mTLS） | `certs/client.crt` |
| `etcd.tls_key` | 客户端私钥文件路径（mTLS） | `certs/client.key` |
| `etcd.tls_ca_cert` | CA 证书文件路径（验证 etcd 服务端） | `certs/ca.crt` |
| `etcd.tls_insecure_skip_verify` | 跳过服务端证书校验（仅测试） | `false` |
| `etcd.tls_server_name` | SNI 校验使用的服务名 | — |
| `server.listen` | Dashboard 监听地址 | `:9090` |
| `server.tls_enable` | 启用 HTTPS | `false` |
| `server.tls_cert` | TLS 证书文件路径 | `certs/server.crt` |
| `server.tls_key` | TLS 私钥文件路径 | `certs/server.key` |
| `server.session_timeout` | Dashboard 会话超时（秒），0 表示不过期 | `3600` |
| `collector.interval` | 指标采集间隔（秒） | `30` |
| `storage.db_path` | SQLite 数据库文件路径 | `data/etcdmonitor.db` |
| `storage.retention_days` | 数据保留天数 | `7` |
| `kv_manager.separator` | KV 树形视图的 key 路径分隔符 | `/` |
| `kv_manager.connect_timeout` | KV 操作的 etcd 连接超时（秒） | `5` |
| `kv_manager.request_timeout` | KV 操作的 etcd 请求超时（秒） | `30` |
| `kv_manager.max_value_size` | KV 操作 value 最大字节数 | `2097152`（2MB） |
| `ops.ops_enable` | 启用 Ops 面板；`false` 时 Ops 标签隐藏且 `/api/ops/*` 返回 403 | `true` |
| `ops.audit_retention_days` | 审计日志保留天数，过期自动清理 | `7` |
| `auth.bcrypt_cost` | 密码哈希 cost（8–14；越界回退为 10 并告警） | `10` |
| `auth.lockout_threshold` | 连续 N 次失败锁定（登录/改密共享） | `5` |
| `auth.lockout_duration_seconds` | 锁定时长（秒） | `900` |
| `auth.min_password_length` | 新密码最短长度 | `8` |
| `log.dir` | 日志目录 | `logs` |
| `log.filename` | 日志文件名 | `etcdmonitor.log` |
| `log.level` | 日志级别：`debug`、`info`、`warn`、`error` | `info` |
| `log.max_size_mb` | 单个日志文件最大 MB（超出后轮转） | `50` |
| `log.max_files` | 保留的日志文件数 | `5` |
| `log.max_age` | 旧日志最长保留天数（0 = 不限） | `30` |
| `log.compress` | 用 gzip 压缩轮转后的日志 | `false` |
| `log.console` | 同时输出到控制台（stdout） | `true` |

## 相关文档

- [TLS 配置](./TLS_CN.md) — Dashboard HTTPS 与 etcd mTLS
- [API 参考](./API_CN.md) — 鉴权保护的 HTTP 接口
- [数据保留策略](./DATA_RETENTION_CN.md) — 存储、保留、自动降采样
- [安全检查清单](./SECURITY_CHECKLIST.md) — 上线前硬化清单
- [返回 README](../README_CN.md)

<!-- 更新此文件时，请同步更新主 README；任何配置项变更必须反映在此文件中。 -->
