# zfs_exporter

A Prometheus exporter for ZFS hosts that combines pool/dataset metrics, share
discovery (NFS/SMB), and service health into one deployable unit. Ships with
pre-built Grafana dashboards and Prometheus alert rules.

## Features

- **Pool metrics** -- size, allocated, free, fragmentation, dedup ratio,
  read-only state, health (state-set)
- **Dataset metrics** -- used, available, referenced space per
  filesystem/volume
- **Share discovery** -- detects `sharenfs`/`sharesmb` properties on datasets
- **Service health** -- checks systemd unit states for ZFS, NFS, SMB, iSCSI
  (configurable)
- **Scan tracking** -- scrub/resilver active status and progress
- **Anomaly detection** -- pre-built alert rules for abnormal dataset growth
  and pool fill prediction
- **Three Grafana dashboards** -- Status (NOC glance), Details (graphs/tables),
  Combined (status + drill-down)
- **19 alert rules + 5 recording rules** -- ready to import into Prometheus

## Installation

### From source

Requires Go 1.25+.

```bash
make build
```

The binary is built to `build/bin/zfs_exporter`.

### From release

Download a pre-built binary from the
[releases page](https://github.com/donaldgifford/zfs_exporter/releases).

## Usage

```bash
# Run with defaults (listens on :9134, scrapes zpool/zfs from PATH)
./zfs_exporter

# Custom listen address and binary paths
./zfs_exporter \
  --web.listen-address=:9100 \
  --zfs.zpool-path=/usr/sbin/zpool \
  --zfs.zfs-path=/usr/sbin/zfs

# Monitor only ZFS and NFS services
./zfs_exporter --host.services=zfs,nfs
```

Visit `http://localhost:9134/` for the landing page, or
`http://localhost:9134/metrics` for Prometheus metrics.

## Configuration

All flags support environment variable overrides.

| Flag | Default | Env Var | Description |
|------|---------|---------|-------------|
| `--web.listen-address` | `:9134` | `ZFS_EXPORTER_LISTEN_ADDRESS` | Address to listen on |
| `--web.metrics-path` | `/metrics` | `ZFS_EXPORTER_METRICS_PATH` | Metrics endpoint path |
| `--log.level` | `info` | `ZFS_EXPORTER_LOG_LEVEL` | Log level (debug, info, warn, error) |
| `--scrape.timeout` | `10s` | `ZFS_EXPORTER_SCRAPE_TIMEOUT` | Timeout budget for all commands per scrape |
| `--zfs.zpool-path` | `zpool` | `ZFS_EXPORTER_ZPOOL_PATH` | Path to `zpool` binary |
| `--zfs.zfs-path` | `zfs` | `ZFS_EXPORTER_ZFS_PATH` | Path to `zfs` binary |
| `--host.services` | `zfs,nfs,smb,iscsi` | `ZFS_EXPORTER_SERVICES` | Comma-separated service keys to monitor |

Precedence: defaults -> CLI flags -> environment variables.

Binary paths are validated at startup. If `zpool` or `zfs` cannot be found or
is not executable, the exporter exits immediately with an error.

## Metrics

Namespace: `zfs`

### Pool Metrics (labels: `pool`)

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_pool_size_bytes` | gauge | Total pool size |
| `zfs_pool_allocated_bytes` | gauge | Allocated space |
| `zfs_pool_free_bytes` | gauge | Free space |
| `zfs_pool_fragmentation_ratio` | gauge | Fragmentation (0-1), NaN if unavailable |
| `zfs_pool_dedup_ratio` | gauge | Deduplication ratio |
| `zfs_pool_readonly` | gauge | 1 if read-only |

### Pool Health (labels: `pool`, `state`)

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_pool_health` | gauge | 1 if pool is in the labeled state (online, degraded, faulted, offline, removed, unavail) |

### Scan Metrics (labels: `pool`)

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_pool_scrub_active` | gauge | 1 if scrub in progress |
| `zfs_pool_resilver_active` | gauge | 1 if resilver in progress |
| `zfs_pool_scan_progress_ratio` | gauge | 0-1 scan progress |

### Dataset Metrics (labels: `dataset`, `pool`, `type`)

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_dataset_used_bytes` | gauge | Space consumed |
| `zfs_dataset_available_bytes` | gauge | Space available |
| `zfs_dataset_referenced_bytes` | gauge | Space referenced |
| `zfs_dataset_share_nfs` | gauge | 1 if NFS sharing enabled |
| `zfs_dataset_share_smb` | gauge | 1 if SMB sharing enabled |

### Service Metrics (labels: `service`)

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_service_up` | gauge | 1 if systemd unit is active |

### Meta Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `zfs_up` | gauge | 1 if ZFS commands succeeded |
| `zfs_scrape_duration_seconds` | gauge | Time to collect all metrics |

## Grafana Dashboards

Three dashboards ship in `contrib/grafana/`:

- **`zfs-status.json`** -- Quick-glance stat panels for NOC screens. Pool
  health, capacity, services, and resilver/scrub status at a glance.
- **`zfs-details.json`** -- Full graphs and tables. Pool capacity over time,
  top datasets, share inventory, anomaly detection panels.
- **`zfs-combined.json`** -- Status panels at the top with collapsible
  drill-down rows for pool details, dataset details, shares/services, and
  anomaly detection.

Import into Grafana via the dashboard import UI. Each dashboard uses
`datasource` and `pool` template variables.

## Prometheus Rules

### Alert Rules

`contrib/prometheus/alerts.yml` contains 19 alert rules covering:

- Exporter health (down, command failures)
- Drive failure/rebuild (degraded, faulted, resilver stalled)
- Pool capacity (80% warning, 90% critical, high fragmentation)
- Services (service down, NFS/SMB share-service mismatches)
- Anomaly detection (abnormal growth with 1d/7d baselines, pool fill
  prediction)

### Recording Rules

`contrib/prometheus/recording_rules.yml` contains 5 recording rules that
pre-compute baselines for anomaly detection:

- 1-day and 7-day averages and standard deviations of dataset usage
- 1-hour smoothed growth rate

Load both files into Prometheus:

```yaml
rule_files:
  - /path/to/alerts.yml
  - /path/to/recording_rules.yml
```

## Service Monitoring

Each service key maps to candidate systemd unit names:

| Key | Units Checked | Purpose |
|-----|---------------|---------|
| `zfs` | `zfs-zed.service` | ZFS Event Daemon |
| `nfs` | `nfs-kernel-server.service`, `nfs-server.service` | NFS server |
| `smb` | `smbd.service`, `smb.service` | Samba |
| `iscsi` | `tgt.service`, `iscsitarget.service` | iSCSI target |

The exporter tries unit names in order per key. If none exist on the host, the
key is silently skipped.

## Permissions

`zpool list`, `zfs list`, and `systemctl is-active` are readable by any user
on standard OpenZFS installations. The exporter does not require root
privileges.

## Development

```bash
make test           # Run tests with race detector
make lint           # Run golangci-lint
make check          # Pre-commit: lint + test
make build          # Build binary
```

Tool versions are managed via [mise](https://mise.jdx.dev/). Run
`mise install` to set up the development environment.

## License

See [LICENSE](LICENSE) for details.
