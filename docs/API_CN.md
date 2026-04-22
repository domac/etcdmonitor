← 返回 [README](../README_CN.md)

# API 参考

etcdmonitor 对外暴露的完整 HTTP API。所有受保护端点需要携带有效会话（登录 `/api/auth/login` 后通过 `Authorization: Bearer <token>` 头传递）。未鉴权端点仅包括 `/api/auth/` 下的三个接口。

## Dashboard APIs

| 端点 | 方法 | 鉴权 | 描述 |
|---|---|---|---|
| `/api/auth/login` | POST | 否 | 本地管理员登录。响应可能包含 `must_change_password=true`（此时不下发 session） |
| `/api/auth/change-password` | POST | 否 | 修改密码（零 token 设计：`username + old_password` 授权） |
| `/api/auth/logout` | POST | 是 | 注销并使 session 失效 |
| `/api/auth/status` | GET | 否 | 查询鉴权需求与 session 状态 |
| `/api/members` | GET | 是 | 列出所有集群成员 |
| `/api/current?member_id=<id>` | GET | 是 | 获取某成员最新指标快照 |
| `/api/range?member_id=<id>&metrics=m1,m2&range=1h` | GET | 是 | 获取指定指标的时间序列 |
| `/api/status` | GET | 是 | 监控系统状态与集群信息 |
| `/api/user/panel-config` | GET | 是 | 获取用户面板显隐与顺序配置 |
| `/api/user/panel-config` | PUT | 是 | 保存用户面板显隐与顺序配置 |
| `/api/debug` | GET | 是 | 调试信息：DB 成员 ID、采集器状态 |

## KV 管理 APIs（v3）

| 端点 | 方法 | 鉴权 | 描述 |
|---|---|---|---|
| `/api/kv/v3/connect` | POST | 是 | 连接并获取集群信息（版本、Leader、DB 大小） |
| `/api/kv/v3/separator` | GET | 是 | 获取 key 路径分隔符 |
| `/api/kv/v3/keys` | GET | 是 | 获取完整 key 树（仅 key，不含 value） |
| `/api/kv/v3/getpath?key=/` | GET | 是 | 获取某路径下的 key 树（递归） |
| `/api/kv/v3/get?key=/foo` | GET | 是 | 获取单个 key 的值与元信息 |
| `/api/kv/v3/put` | PUT | 是 | 创建或更新 key（JSON body：`key`、`value`、`ttl`） |
| `/api/kv/v3/delete` | POST | 是 | 删除 key 或目录（JSON body：`key`、`dir`） |

## KV 管理 APIs（v2）

| 端点 | 方法 | 鉴权 | 描述 |
|---|---|---|---|
| `/api/kv/v2/connect` | POST | 是 | 连接并检查 v2 API 可用性 |
| `/api/kv/v2/separator` | GET | 是 | 获取 key 路径分隔符 |
| `/api/kv/v2/keys` | GET | 是 | 获取完整 key 树（仅 key，不含 value） |
| `/api/kv/v2/getpath?key=/` | GET | 是 | 获取某路径下的 key 树（递归） |
| `/api/kv/v2/get?key=/foo` | GET | 是 | 获取单个 key 的值与元信息 |
| `/api/kv/v2/put` | PUT | 是 | 创建或更新 key（JSON body：`key`、`value`、`ttl`、`dir`） |
| `/api/kv/v2/delete` | POST | 是 | 删除 key 或目录（JSON body：`key`、`dir`） |

## Ops APIs

| 端点 | 方法 | 鉴权 | 描述 |
|---|---|---|---|
| `/api/ops/defragment` | POST | 是 | 对某成员执行碎片整理（JSON body：`member_id`） |
| `/api/ops/snapshot` | GET | 是 | 从某成员下载快照（query：`member_id`） |
| `/api/ops/alarms` | GET | 是 | 列出当前告警 |
| `/api/ops/alarms/disarm` | POST | 是 | 解除指定告警（JSON body：`member_id`、`alarm_type`） |
| `/api/ops/move-leader` | POST | 是 | 把 Leader 迁移到目标成员（JSON body：`target_member_id`） |
| `/api/ops/hashkv` | POST | 是 | 对所有成员执行 HashKV 一致性校验 |
| `/api/ops/compact` | POST | 是 | 集群范围 Revision 压缩（JSON body：`retain_count`、`physical`） |
| `/api/ops/compact/revision` | GET | 是 | 获取集群当前 Revision（供参考） |
| `/api/ops/audit-logs` | GET | 是 | 查询审计日志（query：`page`、`page_size`、`operation`） |

## 鉴权说明

- **独立的本地管理员账号。** Dashboard 登录与 etcd auth 解耦；`config.yaml` 的 `etcd.username` / `etcd.password` 仅被采集器、KV 管理器和 Ops SDK 客户端使用。
- **Session。** 登录成功后响应包含 session token；后续受保护请求需带 `Authorization: Bearer <token>`。
- **登录锁定。** 同一账号连续 5 次失败（登录与改密共享计数）锁定 15 分钟。可在 [CONFIGURATION_CN.md](./CONFIGURATION_CN.md) 的 `auth:` 段调整。
- **首次登录。** 响应若含 `must_change_password=true`，不会下发 session；必须先调 `/api/auth/change-password` 用初始密码完成改密。

## 相关文档

- [配置参考](./CONFIGURATION_CN.md)
- [TLS 配置](./TLS_CN.md)
- [安全检查清单](./SECURITY_CHECKLIST.md)
- [返回 README](../README_CN.md)

<!-- 更新此文件时，请同步更新主 README；任何 API 变更必须反映在此文件中。 -->
