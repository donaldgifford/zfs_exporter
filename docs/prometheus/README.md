# Prometheus Rules

## Overview

The zfs_exporter ships pre-built Prometheus recording rules and alert rules in
`contrib/prometheus/`:

| File | Purpose |
|------|---------|
| `recording_rules.yml` | Pre-computed baselines for anomaly detection dashboards and alerts |
| `alerts.yml` | 21 alert rules covering exporter health, pool health, capacity, services, and anomalies |

## Installation

### 1. Add rule files to Prometheus

Copy or symlink the rule files and add them to your `prometheus.yml`:

```yaml
rule_files:
  - /path/to/contrib/prometheus/recording_rules.yml
  - /path/to/contrib/prometheus/alerts.yml
```

### 2. Reload Prometheus

```bash
# Send SIGHUP
kill -HUP $(pidof prometheus)

# Or use the reload API (requires --web.enable-lifecycle)
curl -X POST http://localhost:9090/-/reload
```

### 3. Verify rules loaded

Check the rules API to confirm both groups are active:

```bash
curl -s http://localhost:9090/api/v1/rules | jq '.data.groups[].name'
```

You should see `zfs_anomaly_baselines` and `zfs_exporter` in the output.

You can also verify in the Prometheus UI at **Status > Rules**.

## Recording Rules

**File:** `contrib/prometheus/recording_rules.yml`

**Group:** `zfs_anomaly_baselines` | **Evaluation interval:** 5 minutes

Recording rules pre-compute rolling averages and standard deviations so that
dashboards and alert rules can reference stable baselines without executing
expensive range queries on every evaluation.

### Rules

| Rule | Expression | Description |
|------|-----------|-------------|
| `zfs:dataset_used_bytes:avg1d` | `avg_over_time(zfs_dataset_used_bytes[1d])` | 1-day rolling average of dataset used bytes. Used by the short-term anomaly alert (`ZfsDatasetAbnormalGrowthShortTerm`) |
| `zfs:dataset_used_bytes:stddev1d` | `stddev_over_time(zfs_dataset_used_bytes[1d])` | 1-day rolling standard deviation. Used to detect short-term spikes |
| `zfs:dataset_used_bytes:avg7d` | `avg_over_time(zfs_dataset_used_bytes[7d])` | 7-day rolling average. Used by the anomaly alert (`ZfsDatasetAbnormalGrowth`) and the Deviation Table panel in Grafana |
| `zfs:dataset_used_bytes:stddev7d` | `stddev_over_time(zfs_dataset_used_bytes[7d])` | 7-day rolling standard deviation. Used to establish normal variance bounds |
| `zfs:dataset_used_bytes:deriv1h` | `deriv(zfs_dataset_used_bytes[1h])` | 1-hour smoothed growth rate in bytes/second. Useful for real-time growth monitoring |

### Why recording rules matter

The `avg_over_time` and `stddev_over_time` functions over 7-day windows scan
thousands of data points on every evaluation. Running these as instant queries
from Grafana panels or alert rules would put significant load on Prometheus at
scrape time. Recording rules evaluate once every 5 minutes and store the result
as a new time series, keeping both dashboards and alerts fast.

### Data availability

- `avg1d` and `stddev1d` require at least **1 day** of scrape history.
- `avg7d` and `stddev7d` require at least **7 days** of scrape history.
- Until enough data accumulates, these rules will return **NaN**. This is
  expected. Alerts that depend on them will not fire, and dashboard panels will
  show NaN or empty values until the window fills.

## Alert Rules

**File:** `contrib/prometheus/alerts.yml`

**Group:** `zfs_exporter`

21 alert rules organized by category. Each alert includes a severity label
(`warning` or `critical`) and annotations with summary/description text.

### Exporter Health

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsExporterDown` | critical | 5m | `up{job="zfs_exporter"} == 0` | The Prometheus scrape target is unreachable. Check that the exporter process is running and the network path is clear |
| `ZfsCommandFailure` | critical | 2m | `zfs_up == 0` | The exporter is reachable but `zpool`/`zfs` commands are failing. Check that ZFS kernel modules are loaded and the exporter user has permission to run ZFS commands |

### Pool Health

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsPoolDegraded` | critical | 1m | `zfs_pool_health{state="degraded"} == 1` | A vdev has failed but the pool is still functional. Run `zpool status` to identify the failed device and replace it |
| `ZfsPoolFaulted` | critical | 0m | `zfs_pool_health{state="faulted"} == 1` | The pool has experienced too many failures and is no longer accessible. Immediate intervention required |
| `ZfsPoolNotOnline` | critical | 1m | `zfs_pool_health{state="online"} == 0` (excluding degraded/faulted) | Pool is in an unexpected state (not online, degraded, or faulted). Check `zpool status` for details |
| `ZfsPoolReadOnly` | warning | 1m | `zfs_pool_readonly == 1` | Pool is mounted read-only. Check for import errors or intentional read-only mounts |
| `ZfsPoolDegradedNotResilvering` | critical | 10m | `zfs_pool_health{state="degraded"} == 1` unless `zfs_pool_resilver_active == 1` | A drive has failed and no rebuild is in progress after 10 minutes. Manual intervention required: the replacement drive may not have been inserted, or the resilver may need to be started manually |

### Resilver/Scrub

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsPoolResilvering` | warning | 0m | `zfs_pool_resilver_active == 1` | A drive rebuild is in progress. Informational; the pool is self-healing. Monitor for completion or stall |
| `ZfsPoolResilverStalled` | critical | 30m | `zfs_pool_resilver_active == 1` and `delta(zfs_pool_scan_progress_ratio[30m]) == 0` | Resilver has been active for 30 minutes with no progress. Check for I/O errors, failed replacement drive, or heavy pool load blocking the resilver |

### Capacity

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsPoolCapacityWarning` | warning | 15m | `(zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.80` | Pool is over 80% full. Plan capacity expansion. ZFS performance degrades significantly above 80% capacity |
| `ZfsPoolCapacityCritical` | critical | 5m | `(zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.90` | Pool is over 90% full. Immediate action needed: delete snapshots, remove data, or expand the pool |
| `ZfsPoolFragmentationHigh` | warning | 1h | `zfs_pool_fragmentation_ratio > 0.50` | Pool fragmentation exceeds 50%. This can degrade write performance. Consider rebalancing or adding capacity |
| `ZfsPoolPredictedFull7d` | warning | 1h | `predict_linear(zfs_pool_free_bytes[7d], 7 * 24 * 3600) < 0` | Based on the 7-day growth trend, the pool will run out of space within 7 days. Review dataset growth and plan expansion |
| `ZfsPoolPredictedFull1d` | critical | 30m | `predict_linear(zfs_pool_free_bytes[1d], 24 * 3600) < 0` | Based on the 1-day growth trend, the pool will fill within 24 hours. Urgent: free space or expand immediately |

### Services

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsServiceDown` | critical | 2m | `zfs_service_up == 0` | A monitored systemd service (ZFS, NFS, SMB, or iSCSI) is not running. Check `systemctl status {service}` |
| `ZfsNFSSharesWithoutService` | critical | 2m | `count(zfs_dataset_share_nfs == 1) > 0` and `zfs_service_up{service="nfs"} == 0` | ZFS datasets are configured with `sharenfs` but the NFS service is down. Clients cannot access their shares. Start the NFS service or investigate why it stopped |
| `ZfsSMBSharesWithoutService` | critical | 2m | `count(zfs_dataset_share_smb == 1) > 0` and `zfs_service_up{service="smb"} == 0` | ZFS datasets are configured with `sharesmb` but the SMB service is down. Clients cannot access their shares. Start the SMB service or investigate why it stopped |

### Anomaly Detection

| Alert | Severity | For | Expression | Description |
|-------|----------|-----|-----------|-------------|
| `ZfsDatasetAbnormalGrowth` | warning | 1h | Current usage deviates > 2 standard deviations from the 7-day average, and exceeds the minimum threshold (1 GiB or 10% of average) | A dataset is growing abnormally compared to its 7-day baseline. Investigate what is writing unexpectedly large amounts of data. **Requires recording rules** |
| `ZfsDatasetAbnormalGrowthShortTerm` | warning | 30m | Current usage deviates > 3 standard deviations from the 1-day average, and exceeds the minimum threshold (1 GiB or 10% of average) | A dataset is spiking compared to its 1-day baseline. More sensitive than the 7-day alert for catching rapid changes. **Requires recording rules** |

The anomaly alerts use a dual threshold: the deviation must exceed both a
statistical bound (standard deviations from the mean) and an absolute minimum
floor (1 GiB or 10% of the average, whichever is larger). This prevents
false positives on tiny datasets with naturally low variance.

## Troubleshooting

### Rules not loading

1. Check the `rule_files` paths in `prometheus.yml` are correct and accessible
   by the Prometheus process.
2. Validate YAML syntax:
   ```bash
   promtool check rules contrib/prometheus/alerts.yml
   promtool check rules contrib/prometheus/recording_rules.yml
   ```
3. Check Prometheus logs for rule loading errors after reload.
4. Verify in the Prometheus UI at **Status > Rules** that both groups appear.

### Recording rule metrics missing

1. Query `zfs:dataset_used_bytes:avg7d` in Prometheus. If it returns no
   results:
   - The recording rules file may not be loaded (see above).
   - There may be no `zfs_dataset_used_bytes` source data. Check that the
     exporter is running and being scraped.
2. If the query returns NaN, the recording rule is loaded but does not yet have
   enough history. The 7-day rules need 7 days of continuous scrape data before
   producing numeric results.
3. Check the rule evaluation status in **Status > Rules** for errors.

### Alerts not firing

1. **Check metric availability.** Each alert depends on specific metrics
   existing. Query the base metric (e.g., `zfs_pool_health`) in Prometheus to
   confirm data is present.
2. **Check label matching.** Alert expressions filter on labels like
   `state="degraded"` or `service="nfs"`. Verify that your exporter emits
   metrics with matching label values.
3. **Check the `for` duration.** Most alerts require the condition to persist
   for a specified duration (e.g., 5m, 15m, 1h) before firing. The alert will
   show as "pending" in the Prometheus Alerts UI during this window.
4. **Check recording rule dependencies.** The anomaly alerts
   (`ZfsDatasetAbnormalGrowth`, `ZfsDatasetAbnormalGrowthShortTerm`) depend on
   recording rules. If the recording rule metrics don't exist, these alerts
   will never fire.

### Customizing thresholds

Alert thresholds are defined directly in the `expr` field of each rule. Common
values to adjust:

| Alert | Default | Value to change |
|-------|---------|-----------------|
| `ZfsPoolCapacityWarning` | 80% | Change `0.80` in the expression |
| `ZfsPoolCapacityCritical` | 90% | Change `0.90` in the expression |
| `ZfsPoolFragmentationHigh` | 50% | Change `0.50` in the expression |
| `ZfsPoolPredictedFull7d` | 7 days | Change `7 * 24 * 3600` to a different number of days |
| `ZfsPoolPredictedFull1d` | 1 day | Change `24 * 3600` to a different duration |
| `ZfsDatasetAbnormalGrowth` | 2 sigma, 1 GiB floor | Change `2 *` for sensitivity, `1073741824` for floor |
| `ZfsDatasetAbnormalGrowthShortTerm` | 3 sigma, 1 GiB floor | Change `3 *` for sensitivity, `1073741824` for floor |

After editing `contrib/prometheus/alerts.yml`, reload Prometheus and verify the
changes in **Status > Rules**.

> **Note:** If you regenerate rules with `make dashboards`, your manual edits
> will be overwritten. To make persistent threshold changes, modify the
> generator source in `tools/dashgen/` and regenerate.
