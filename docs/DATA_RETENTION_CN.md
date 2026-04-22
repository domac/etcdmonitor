← 返回 [README](../README_CN.md)

# 数据保留与存储

etcdmonitor 将时间序列指标存入单个 **按成员分表** 的 SQLite 数据库，内部自管理保留策略，无需外部 cron 或 agent。

## 保留策略

- 超过 `retention_days`（默认 **7 天**）的数据会自动清理。
- 清理任务 **每小时** 运行一次。
- 当单次删除超过 10,000 行时，执行完整 `VACUUM` 回收磁盘。
- 较小删除使用 SQLite `incremental_vacuum`，开销可忽略。

在 `config.yaml` 调整保留窗口：

```yaml
storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7
```

存储相关的完整参数见 [CONFIGURATION_CN.md](./CONFIGURATION_CN.md#参数说明)。

## 自动降采样

大时间范围会自动降采样，使 Dashboard 即使查询 7 天数据也能秒级响应：

| 时间范围 | 聚合粒度 | 每指标数据点 | 方法 |
|---|---|---|---|
| ≤ 30 分钟 | 不聚合 | ~60 | 原始数据 |
| ≤ 2 小时 | 30 秒 | ~240 | `AVG()` |
| ≤ 12 小时 | 2 分钟 | ~360 | `AVG()` |
| ≤ 48 小时 | 5 分钟 | ~576 | `AVG()` |
| > 48 小时 | 10 分钟 | ~1,008 | `AVG()` |

降采样对前端透明 —— `/api/range` 会根据请求的时间范围自动选择桶大小。

## 存储估算

30 秒采集间隔、默认保留下的大致真实数据：

| 集群规模 | 7 天行数 | DB 文件大小 |
|---|---|---|
| 1 节点 | ~1 M | ~50 MB |
| 3 节点 | ~3 M | ~150 MB |
| 5 节点 | ~5 M | ~250 MB |

> 保留与降采样 **按成员** 生效。在 Dashboard 中切换成员仅是一次廉价查询，
> 不会丢数据。只有 `config.yaml` 中的 `etcd.endpoint` 指向 **另一个集群** 时，
> 系统才会全量清理，以避免多个集群数据混合。

## 端点变更检测

当 `etcd.endpoint` 指向不同集群时，etcdmonitor 在启动时检测并清空历史数据以保证干净起点。这是有意为之的：两个无关集群的时间序列混在一起会让图表毫无意义。

## 相关文档

- [配置参考](./CONFIGURATION_CN.md)
- [API 参考](./API_CN.md)
- [安全检查清单](./SECURITY_CHECKLIST.md)
- [返回 README](../README_CN.md)

<!-- 更新此文件时，请同步更新主 README；任何保留策略变更必须反映在此文件中。 -->
