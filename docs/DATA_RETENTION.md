← Back to [README](../README.md)

# Data Retention & Storage

etcdmonitor stores time-series metrics in a single **per-member SQLite**
database and self-manages retention without any external cron or agent.

## Retention policy

- Data older than `retention_days` (default: **7 days**) is automatically
  purged.
- Cleanup runs **every hour**.
- When more than 10,000 rows are deleted in one pass, a full `VACUUM`
  reclaims disk space.
- Smaller deletions use SQLite `incremental_vacuum` for minimal overhead.

Tune the retention window in `config.yaml`:

```yaml
storage:
  db_path: "data/etcdmonitor.db"
  retention_days: 7
```

See [CONFIGURATION.md](./CONFIGURATION.md#parameter-reference) for every
storage-related setting.

## Auto downsampling

Large time ranges are automatically downsampled so the dashboard stays
responsive — you should never have to wait more than a second on a query,
even for 7 days of data:

| Time range | Aggregation | Data points per metric | Method |
|---|---|---|---|
| ≤ 30 min | None | ~60 | Raw data |
| ≤ 2 hours | 30 sec | ~240 | `AVG()` |
| ≤ 12 hours | 2 min | ~360 | `AVG()` |
| ≤ 48 hours | 5 min | ~576 | `AVG()` |
| > 48 hours | 10 min | ~1,008 | `AVG()` |

Downsampling is transparent to the frontend — the `/api/range` endpoint
picks the bucket based on the requested range.

## Storage estimates

Rough, real-world numbers from 30-second collection at default retention:

| Cluster size | 7-day rows | DB file size |
|---|---|---|
| 1 node | ~1 M | ~50 MB |
| 3 nodes | ~3 M | ~150 MB |
| 5 nodes | ~5 M | ~250 MB |

> Data-retention and downsampling are applied on a **per-member** basis.
> Switching members in the Dashboard is a cheap query against an existing
> table — no data is dropped. Only changing the `etcd.endpoint` in
> `config.yaml` (pointing to a different cluster) triggers a full data
> wipe to avoid mixing clusters.

## Endpoint change detection

When `etcd.endpoint` in `config.yaml` changes (points to a different
cluster), etcdmonitor detects this at startup and purges all historical
data for a clean slate. This is intentional: mixing time-series from two
unrelated clusters would produce meaningless charts.

## Related documents

- [Configuration reference](./CONFIGURATION.md)
- [API reference](./API.md)
- [Security checklist](./SECURITY_CHECKLIST.md)
- [Back to README](../README.md)

<!-- Keep this file in sync with the main README; any change to retention policy must be reflected here. -->
