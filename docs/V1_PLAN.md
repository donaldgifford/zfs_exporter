# V1 Plan: zfs_exporter

## Goal

Ship a Prometheus exporter, Grafana dashboards, and Prometheus alert rules as a
complete monitoring package for operators running ZFS on Debian/Linux hosts.

V1 answers these questions at a glance:

1. **Is my pool healthy?** (health status, read-only state)
2. **Am I running out of space?** (pool capacity, dataset usage)
3. **Has a drive failed? Is it rebuilding?** (degraded state, resilver/scrub
   status and progress)
4. **What shares exist and are their services running?** (NFS/SMB share
   inventory, service health)
5. **Is anything down?** (ZFS daemon, NFS, SMB, iSCSI service status)
6. **Is anything growing abnormally?** (anomaly detection on dataset usage with
   1-day and 7-day baselines)

The exporter runs directly on the ZFS host. It executes `zpool` and `zfs` CLI
commands, checks systemd service states, and serves Prometheus metrics over
HTTP. Pre-built Grafana dashboards and alert rules ship alongside the binary.

### Success Criteria

V1 is complete when all of the following are true:

1. **Binary builds and runs:** `make build` produces a static binary that starts,
   listens on `:9134`, and serves `/metrics` with valid Prometheus exposition
   format.
2. **ZFS metrics collected:** Pool size/allocated/free/fragmentation/dedup/
   readonly/health and dataset used/available/referenced/share_nfs/share_smb
   metrics are emitted from parsed `zpool list` and `zfs list` output.
3. **Scan metrics collected:** Scrub/resilver active status and progress ratio
   are emitted from parsed `zpool status` output.
4. **Service health collected:** `zfs_service_up` metrics are emitted for each
   service in the configured `--host.services` list via `systemctl is-active`.
5. **Binary path validation works:** `--zfs.zpool-path` and `--zfs.zfs-path`
   flags are validated at startup; missing or non-executable binaries cause an
   immediate exit with a clear error.
6. **Tests pass:** `make test` passes with race detector enabled. Parser tests
   cover happy path, edge cases (NaN fragmentation, volume share properties),
   and malformed input. Client tests cover command success and failure. Collector
   tests use `testutil.CollectAndCompare`. Coverage meets the 60% target.
7. **Lint passes:** `make lint` passes with the strict `.golangci.yml` config.
8. **Alert rules valid:** `contrib/prometheus/alerts.yml` contains all planned
   alert rules (exporter health, drive failure/rebuild, pool capacity, services,
   share mismatches, anomaly detection with `max(1 GiB, 10%)` floor, pool fill
   prediction).
9. **Recording rules valid:** `contrib/prometheus/recording_rules.yml` contains
   1-day and 7-day baseline recording rules for anomaly detection.
10. **Three Grafana dashboards ship:** `contrib/grafana/` contains
    `zfs-status.json`, `zfs-details.json`, and `zfs-combined.json` with
    appropriate panels, variables, and thresholds.
11. **Graceful shutdown works:** The binary handles SIGINT/SIGTERM and shuts down
    the HTTP server cleanly.
12. **Error handling correct:** Pool command failure sets `up=0` and returns
    early. Dataset/scan/service failures log warnings and continue with partial
    metrics.

### Why Not Use Existing Exporters?

The [pdf/zfs_exporter](https://github.com/pdf/zfs_exporter) provides pool and
dataset property metrics via CLI commands. The
[node_exporter](https://github.com/prometheus/node_exporter) ZFS collector reads
`/proc/spl/kstat/zfs/` for ARC and kernel-level stats.

Neither provides the full operational picture for a ZFS NAS:

- No share visibility (which datasets have `sharenfs`/`sharesmb` enabled)
- No service health (is NFS/SMB/iSCSI actually running)
- No pre-built dashboards or alert rules
- No correlation between "shares are configured" and "service is up"
- Metrics exist in isolation without operational context

Our exporter fills these gaps by combining ZFS metrics, share discovery, service
health, dashboards, and alerts into one deployable unit.

---

## How ZFS Data Collection Works

ZFS exposes data through three mechanisms:

| Method                                  | Pros                                                                    | Cons                                                                  |
| --------------------------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------- |
| CLI commands (`zpool list`, `zfs list`) | Portable (Linux + macOS), no CGo, stable output format with `-Hp` flags | Spawns processes per scrape                                           |
| procfs (`/proc/spl/kstat/zfs/`)         | Zero-overhead file reads, kernel-level stats (ARC, I/O)                 | Linux-only, format can vary between OpenZFS versions                  |
| libzfs CGo bindings                     | Most complete                                                           | Requires CGO_ENABLED=1 (breaks our static binary build), unstable API |

### V1 Decision: CLI Commands + systemctl

We use `zpool list` and `zfs list` with parseable flags for ZFS data, and
`systemctl is-active` for service health.

ZFS command flags:

- `-H` suppresses headers, outputs tab-separated fields
- `-p` outputs exact values (bytes as integers, percentages as integers, no unit
  suffixes)
- `-o` selects explicit columns (avoids relying on default column order, which
  varies between OpenZFS versions)

This approach works with CGO_ENABLED=0 (static binaries), is portable across
Linux and macOS, and has a stable, well-documented output format. The overhead
of spawning a few processes per scrape is negligible for a metrics endpoint
scraped every 15-60 seconds.

procfs-based collection (ARC stats, I/O histograms) is deferred to V2.

### Commands

**Pool properties:**

```bash
zpool list -Hp -o name,size,alloc,free,frag,cap,dedup,health,readonly
```

Output (tab-separated, one pool per line):

```
tank 10737418240 5368709120 5368709120 33 50 1.00 ONLINE off
backup 5368709120 1073741824 4294967296 - 20 1.00 ONLINE off
```

Notes:

- `frag` can be `-` on pools without enough history; parse as NaN
- `cap` is an integer percentage (e.g. `50` means 50%); we do not need to expose
  this as a metric since it can be derived from `alloc / size`
- `dedup` is a float ratio (e.g. `1.00`)
- `health` is one of: ONLINE, DEGRADED, FAULTED, OFFLINE, REMOVED, UNAVAIL
- `readonly` is `on` or `off`

**Dataset properties (including share info):**

```bash
zfs list -Hp -o name,used,avail,refer,type,sharenfs,sharesmb -t filesystem,volume
```

Output (tab-separated, one dataset per line):

```
tank 5368709120 5368709120 262144 filesystem off off
tank/media 4294967296 5368709120 4294967296 filesystem on off
tank/backups 1073741824 5368709120 1073741824 filesystem rw=@10.0.0.0/24 off
tank/shared 536870912 5368709120 536870912 filesystem off on
tank/zvol0 1073741824 5368709120 1073741824 volume - -
```

Notes:

- The pool name is extracted from the dataset name (everything before the first
  `/`, or the full name for root datasets)
- `-t filesystem,volume` excludes snapshots and bookmarks (noisy; can be added
  later)
- `sharenfs` is `off`, `on`, or an NFS options string (e.g. `rw=@10.0.0.0/24`).
  Anything other than `off` means NFS sharing is enabled
- `sharesmb` is `off`, `on`, or SMB options. Anything other than `off` means SMB
  sharing is enabled
- Volumes report `-` for share properties (sharing doesn't apply to zvols)

**Scan status (resilver/scrub):**

```bash
zpool status
```

Multi-line output, parsed per pool. The `scan:` line indicates current or last
scan activity. Examples:

```
# No scan ever requested
  scan: none requested

# Scrub in progress
  scan: scrub in progress since Sun Jul 25 16:07:49 2025
    374G scanned at 161M/s, 340G issued at 146M/s, 703G total
    0B repaired, 48.36% done, 00:42:27 to go

# Resilver in progress (drive replacement/rebuild)
  scan: resilver in progress since Mon Feb  3 10:00:00 2025
    1.23G scanned at 100M/s, 500M issued at 50M/s, 5.00G total
    500M resilvered, 10.00% done, 0 days 01:30:00 to go

# Completed scrub
  scan: scrub repaired 0B in 01:23:45 with 0 errors on Sun Feb  2 00:24:01 2025
```

Parsing approach:

- Split output by `pool:` sections to associate with each pool
- Regex match `scan:\s+(scrub|resilver) in progress` to detect active scans
- Regex match `(\d+\.?\d*)%\s+done` to extract progress percentage
- Anything else (including `none requested` or completed scans) = no active scan

This is more complex than `zpool list` parsing but scoped to just the `scan:`
line, not the full vdev tree.

**Service health:**

```bash
systemctl is-active <unit-name>
```

Returns `active`, `inactive`, `failed`, or `activating` (exit code 0 only for
`active`). Checked for each configured service unit.

---

## V1 Metrics

Namespace: `zfs`

### Pool Metrics

Labels: `pool`

| Metric                         | Type  | Description                                  |
| ------------------------------ | ----- | -------------------------------------------- |
| `zfs_pool_size_bytes`          | gauge | Total pool size in bytes                     |
| `zfs_pool_allocated_bytes`     | gauge | Allocated space in bytes                     |
| `zfs_pool_free_bytes`          | gauge | Free space in bytes                          |
| `zfs_pool_fragmentation_ratio` | gauge | Pool fragmentation (0-1), NaN if unavailable |
| `zfs_pool_dedup_ratio`         | gauge | Deduplication ratio                          |
| `zfs_pool_readonly`            | gauge | 1 if pool is read-only, 0 otherwise          |

Labels: `pool`, `state`

| Metric            | Type  | Description                                                                                                  |
| ----------------- | ----- | ------------------------------------------------------------------------------------------------------------ |
| `zfs_pool_health` | gauge | 1 if pool is in the labeled state, 0 otherwise. States: online, degraded, faulted, offline, removed, unavail |

The state-set pattern for health enables clean alerting:

```promql
# Alert on any pool not online
zfs_pool_health{state="online"} == 0
```

### Pool Scan Metrics

Labels: `pool`

| Metric                         | Type  | Description                                           |
| ------------------------------ | ----- | ----------------------------------------------------- |
| `zfs_pool_scrub_active`        | gauge | 1 if a scrub is in progress, 0 otherwise              |
| `zfs_pool_resilver_active`     | gauge | 1 if a resilver (rebuild) is in progress, 0 otherwise |
| `zfs_pool_scan_progress_ratio` | gauge | 0-1 progress of active scan, 0 if no scan active      |

These enable critical drive failure/rebuild alerting:

```promql
# Pool is degraded and NOT rebuilding (needs manual intervention)
(zfs_pool_health{state="degraded"} == 1) unless (zfs_pool_resilver_active == 1)

# Resilver in progress (informational)
zfs_pool_resilver_active == 1
```

### Dataset Metrics

Labels: `dataset`, `type`, `pool`

| Metric                         | Type  | Description                                                  |
| ------------------------------ | ----- | ------------------------------------------------------------ |
| `zfs_dataset_used_bytes`       | gauge | Space consumed by dataset                                    |
| `zfs_dataset_available_bytes`  | gauge | Space available to dataset                                   |
| `zfs_dataset_referenced_bytes` | gauge | Space referenced by dataset                                  |
| `zfs_dataset_share_nfs`        | gauge | 1 if NFS sharing is enabled (`sharenfs != off`), 0 otherwise |
| `zfs_dataset_share_smb`        | gauge | 1 if SMB sharing is enabled (`sharesmb != off`), 0 otherwise |

Share metrics enable two key queries:

```promql
# List all NFS-shared datasets
zfs_dataset_share_nfs == 1

# Alert: NFS shares configured but NFS service is down
(count(zfs_dataset_share_nfs == 1) > 0) and (zfs_service_up{service="nfs"} == 0)
```

### Service Metrics

Labels: `service`

| Metric           | Type  | Description                              |
| ---------------- | ----- | ---------------------------------------- |
| `zfs_service_up` | gauge | 1 if systemd unit is active, 0 otherwise |

The service list is configured via `--host.services` (default: `zfs,nfs,smb,iscsi`).
Operators can customize which services are monitored. Each service key maps to
one or more candidate systemd unit names:

| Service Key | Systemd Unit(s) Checked                           | Purpose                                                 |
| ----------- | ------------------------------------------------- | ------------------------------------------------------- |
| `zfs`       | `zfs-zed.service`                                 | ZFS Event Daemon                                        |
| `nfs`       | `nfs-kernel-server.service`, `nfs-server.service` | NFS server (tries Debian name, falls back to RHEL name) |
| `smb`       | `smbd.service`, `smb.service`                     | Samba SMB server                                        |
| `iscsi`     | `tgt.service`, `iscsitarget.service`              | iSCSI target                                            |

For each service key in the configured list, the exporter tries the first unit
name. If that unit does not exist on the system, it tries the next. If none
exist, the service key is silently skipped (not every host runs every service).
Service keys not in the configured list are never checked.

### Exporter Meta-Metrics

No extra labels.

| Metric                        | Type  | Description                               |
| ----------------------------- | ----- | ----------------------------------------- |
| `zfs_up`                      | gauge | 1 if ZFS commands succeeded, 0 on failure |
| `zfs_scrape_duration_seconds` | gauge | Time taken to collect all metrics         |

### Total Metric Count (V1)

For a system with P pools, D datasets, and S detected services:
`7*P + 6*P (health states) + 3*P (scan) + 5*D + S + 2`

Example: 2 pools, 10 datasets, 3 services = 14 + 12 + 6 + 50 + 3 + 2 = 87
metrics.

---

## Package Design

### `pkg/zfs/` - ZFS Client

Executes ZFS CLI commands and parses output. No HTTP, no libzfs.

```
pkg/zfs/
    zfs.go           # Client struct, Runner type, NewClient, DefaultRunner
    pool.go          # Pool type, GetPools, parsePools
    dataset.go       # Dataset type, GetDatasets, parseDatasets
    scan.go          # ScanStatus type, GetScanStatuses, parseScanStatuses
    pool_test.go     # Table-driven parser tests + client tests
    dataset_test.go
    scan_test.go     # Scan status parsing tests (resilver/scrub fixtures)
```

**Runner function type** (replaces HTTP client from GUIDE.md):

```go
// Runner executes a command and returns stdout.
// Production: wraps exec.CommandContext.
// Tests: returns fixture data.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func DefaultRunner() Runner {
    return func(ctx context.Context, name string, args ...string) ([]byte, error) {
        return exec.CommandContext(ctx, name, args...).Output()
    }
}
```

**Client:**

```go
type Client struct {
    runner Runner
    logger *slog.Logger
}

func NewClient(runner Runner, logger *slog.Logger) *Client
func (c *Client) GetPools(ctx context.Context) ([]Pool, error)
func (c *Client) GetDatasets(ctx context.Context) ([]Dataset, error)
func (c *Client) GetScanStatuses(ctx context.Context) ([]ScanStatus, error)
```

**Types:**

```go
type Pool struct {
    Name          string
    Size          uint64
    Allocated     uint64
    Free          uint64
    Fragmentation float64 // 0-1, NaN if unavailable
    DedupRatio    float64
    Health        string  // ONLINE, DEGRADED, etc.
    ReadOnly      bool
}

type Dataset struct {
    Name       string
    Pool       string  // extracted from Name: "tank/data" -> "tank"
    Used       uint64
    Available  uint64
    Referenced uint64
    Type       string  // "filesystem" or "volume"
    ShareNFS   bool    // true if sharenfs != "off" and != "-"
    ShareSMB   bool    // true if sharesmb != "off" and != "-"
}

type ScanStatus struct {
    Pool     string
    Scrub    bool    // true if scrub in progress
    Resilver bool    // true if resilver in progress
    Progress float64 // 0-1 scan progress, 0 if no active scan
}
```

**Parsing** is separated from execution for testability. The parse functions are
unexported (same-package tests can access them):

```go
func parsePools(data []byte) ([]Pool, error)
func parseDatasets(data []byte) ([]Dataset, error)
func parseScanStatuses(data []byte) ([]ScanStatus, error)
```

### `pkg/host/` - Host Service Checker

Checks systemd service unit states. Thin package, reuses the `Runner` type from
`pkg/zfs/`.

```
pkg/host/
    service.go       # ServiceChecker, CheckService, ServiceStatus type
    service_test.go  # Tests with injected Runner
```

```go
type ServiceStatus struct {
    Name   string // service key (e.g. "nfs")
    Active bool   // true if systemd unit reports "active"
}

type ServiceChecker struct {
    runner zfs.Runner
    logger *slog.Logger
}

func NewServiceChecker(runner zfs.Runner, logger *slog.Logger) *ServiceChecker

// CheckServices takes a map of service keys to candidate unit names.
// For each key, tries units in order until one exists. Returns status
// for each found service.
func (s *ServiceChecker) CheckServices(ctx context.Context, services map[string][]string) ([]ServiceStatus, error)
```

### `collector/` - Prometheus Collector

```
collector/
    collector.go       # Collector struct, Describe, Collect
    collector_test.go  # Tests with testutil.CollectAndCompare
```

Follows the GUIDE.md collector pattern:

- Metric descriptors defined once in constructor
- `Collect()` calls `client.GetPools()`, `client.GetDatasets()`,
  `client.GetScanStatuses()`, and `serviceChecker.CheckServices()`
- Pool failure (required) sets `up=0` and returns early
- Dataset failure (optional) logs warning, continues without dataset metrics
- Scan status failure (optional) logs warning, continues without scan metrics
- Service check failure (optional) logs warning, continues without service
  metrics
- Health state-set emits one metric per pool per possible state

### `config/` - Configuration

```
config/
    config.go    # Config struct, NewConfig, ApplyEnvironment, Validate
    errors.go    # Sentinel errors
```

### `exporter/` - HTTP Handlers

```
exporter/
    exporter.go  # LandingPageHandler
```

### `cmd/zfs_exporter/` - Entry Point

```
cmd/zfs_exporter/
    main.go      # Flag parsing, client/collector wiring, HTTP server, shutdown
```

Wiring sequence (follows GUIDE.md main pattern):

1. Parse kingpin flags
2. Setup slog logger
3. Apply env var overrides, validate config
4. Create `zfs.Client` with `DefaultRunner()`
5. Create `host.ServiceChecker` with `DefaultRunner()`
6. Create and register `collector.Collector`
7. Start HTTP server with all four timeouts
8. Graceful shutdown on SIGINT/SIGTERM

### `contrib/` - Dashboards, Alerts, and Recording Rules

```
contrib/
    grafana/
        zfs-status.json      # Status dashboard (quick-glance stat panels)
        zfs-details.json     # Details dashboard (all graphs and tables)
        zfs-combined.json    # Combined dashboard (status + expandable drill-down)
    prometheus/
        alerts.yml           # Prometheus alert rules
        recording_rules.yml  # Recording rules for anomaly detection baselines
```

These ship in the repo and are referenced from the README. They are not embedded
in the binary.

---

## Configuration Flags

| Flag                   | Default    | Env Override                  | Description                                              |
| ---------------------- | ---------- | ----------------------------- | -------------------------------------------------------- |
| `--web.listen-address` | `:9134`    | `ZFS_EXPORTER_LISTEN_ADDRESS` | Address to listen on                                     |
| `--web.metrics-path`   | `/metrics` | `ZFS_EXPORTER_METRICS_PATH`   | Path for metrics endpoint                                |
| `--log.level`          | `info`     | `ZFS_EXPORTER_LOG_LEVEL`      | Log level (debug, info, warn, error)                     |
| `--scrape.timeout`     | `10s`      | `ZFS_EXPORTER_SCRAPE_TIMEOUT` | Total timeout budget for all commands in a single scrape |
| `--zfs.zpool-path`     | `zpool`    | `ZFS_EXPORTER_ZPOOL_PATH`     | Path to `zpool` binary                                   |
| `--zfs.zfs-path`       | `zfs`      | `ZFS_EXPORTER_ZFS_PATH`       | Path to `zfs` binary                                     |
| `--host.services`      | `zfs,nfs,smb,iscsi` | `ZFS_EXPORTER_SERVICES` | Comma-separated list of service keys to monitor |

Port 9134 is the conventionally registered Prometheus port for ZFS exporters.

**Binary path validation:** At startup, the exporter resolves `--zfs.zpool-path`
and `--zfs.zfs-path` (via `exec.LookPath` for bare names, or `os.Stat` for
absolute paths) and verifies the files exist and are executable. If validation
fails, the exporter logs an error and exits immediately. This catches
misconfiguration early rather than failing silently on the first scrape.

---

## Grafana Dashboards

Three dashboards ship in `contrib/grafana/`. All share the same template
variables: `datasource` (Prometheus data source selector), `pool` (multi-select
from `zfs_pool_size_bytes` label values).

### Dashboard 1: Status (`zfs-status.json`)

A quick-glance dashboard designed for NOC screens and alert triage. Primarily
stat panels tied to alert thresholds so operators can see red/green at a glance.

- Pool health stat panels (green/red per pool, maps to `ZfsPoolNotOnline` alert)
- Pool capacity stat panels (green/yellow/red per pool, thresholds at 80%/90%)
- Resilver/scrub status (idle/active/progress per pool)
- Service status stat panels (green/red per service)
- Share/service mismatch indicator (NFS shares configured but service down, etc.)
- Exporter up/down indicator
- Pool predicted days-until-full stat panels

### Dashboard 2: Details (`zfs-details.json`)

All graphs and tables for detailed metric exploration. Used when drilling into
specific pools, datasets, or trends.

**Row 1 - Pool Capacity:**

- Pool usage over time (time series, stacked allocated/free)
- Pool usage bar gauges (allocated vs free per pool)
- Fragmentation over time per pool

**Row 2 - Dataset Usage:**

- Top datasets by used space (bar chart or table, sorted descending)
- Dataset available space (table with pool/type columns)
- Dataset usage over time (time series per dataset)

**Row 3 - Share Inventory:**

- Datasets with NFS enabled (table, filtered to `share_nfs == 1`)
- Datasets with SMB enabled (table, filtered to `share_smb == 1`)
- Service status timeline

**Row 4 - Anomaly Detection:**

- Dataset daily growth rate (time series: `deriv(zfs_dataset_used_bytes[1h])`
  converted to bytes/day, per dataset)
- Datasets outside normal range (table: datasets where current usage deviates
  beyond 2 standard deviations from 7-day average)
- Pool days-until-full prediction time series
  (`predict_linear(zfs_pool_free_bytes[7d], 30*24*3600)`)

### Dashboard 3: Combined (`zfs-combined.json`)

The all-in-one operational dashboard. Status panels at the top for immediate
health assessment, followed by expandable/collapsible row panels for drill-down.
This is likely the dashboard most operators will keep long-term.

**Top section (always visible):**

- Pool health stat panels (green/red per pool)
- Pool capacity stat panels with thresholds (80%/90%)
- Service status stat panels
- Resilver/scrub status per pool
- Pool predicted days-until-full
- Exporter up/down

**Expandable Row: Pool Details** (collapsed by default)

- Pool usage over time, bar gauges, fragmentation

**Expandable Row: Dataset Details** (collapsed by default)

- Top datasets, available space, usage over time

**Expandable Row: Shares & Services** (collapsed by default)

- NFS/SMB share tables, service timeline, mismatch indicators

**Expandable Row: Anomaly Detection** (collapsed by default)

- Growth rate, deviation table, prediction time series

---

## Prometheus Alert Rules and Recording Rules

### Alert Rules (`contrib/prometheus/alerts.yml`)

```yaml
groups:
  - name: zfs_exporter
    rules:
      # --- Exporter health ---

      - alert: ZfsExporterDown
        expr: up{job="zfs_exporter"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "ZFS exporter is down on {{ $labels.instance }}"

      - alert: ZfsCommandFailure
        expr: zfs_up == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "ZFS commands failing on {{ $labels.instance }}"

      # --- Drive failure and rebuild ---

      - alert: ZfsPoolDegraded
        expr: zfs_pool_health{state="degraded"} == 1
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "ZFS pool {{ $labels.pool }} is DEGRADED (drive failure)"
          description:
            "A vdev in pool {{ $labels.pool }} has failed. Check zpool status
            for details."

      - alert: ZfsPoolFaulted
        expr: zfs_pool_health{state="faulted"} == 1
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "ZFS pool {{ $labels.pool }} is FAULTED"
          description:
            "Pool {{ $labels.pool }} has experienced too many failures and is no
            longer accessible."

      - alert: ZfsPoolDegradedNotResilvering
        expr: |
          (zfs_pool_health{state="degraded"} == 1)
            unless on(pool)
          (zfs_pool_resilver_active == 1)
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Pool {{ $labels.pool }} is degraded but NOT resilvering"
          description:
            "A drive has failed in pool {{ $labels.pool }} and no resilver is in
            progress. Manual intervention required."

      - alert: ZfsPoolResilvering
        expr: zfs_pool_resilver_active == 1
        for: 0m
        labels:
          severity: warning
        annotations:
          summary:
            "Pool {{ $labels.pool }} resilver in progress ({{ $value |
            humanizePercentage }} complete)"
          description:
            "A drive rebuild is underway for pool {{ $labels.pool }}."

      - alert: ZfsPoolResilverStalled
        expr: |
          (zfs_pool_resilver_active == 1)
            and
          (delta(zfs_pool_scan_progress_ratio[30m]) == 0)
        for: 30m
        labels:
          severity: critical
        annotations:
          summary: "Resilver stalled on pool {{ $labels.pool }}"
          description: "Resilver progress has not advanced in 30 minutes."

      # --- Pool health (catch-all for non-online states) ---

      - alert: ZfsPoolNotOnline
        expr: |
          (zfs_pool_health{state="online"} == 0)
            unless on(pool)
          (zfs_pool_health{state="degraded"} == 1)
            unless on(pool)
          (zfs_pool_health{state="faulted"} == 1)
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "ZFS pool {{ $labels.pool }} is not ONLINE"

      - alert: ZfsPoolReadOnly
        expr: zfs_pool_readonly == 1
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "ZFS pool {{ $labels.pool }} is read-only"

      # --- Capacity ---

      - alert: ZfsPoolCapacityWarning
        expr: (zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.80
        for: 15m
        labels:
          severity: warning
        annotations:
          summary:
            "ZFS pool {{ $labels.pool }} is {{ $value | humanizePercentage }}
            full"

      - alert: ZfsPoolCapacityCritical
        expr: (zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.90
        for: 5m
        labels:
          severity: critical
        annotations:
          summary:
            "ZFS pool {{ $labels.pool }} is {{ $value | humanizePercentage }}
            full"

      - alert: ZfsPoolFragmentationHigh
        expr: zfs_pool_fragmentation_ratio > 0.50
        for: 1h
        labels:
          severity: warning
        annotations:
          summary:
            "ZFS pool {{ $labels.pool }} fragmentation is {{ $value |
            humanizePercentage }}"

      # --- Services ---

      - alert: ZfsServiceDown
        expr: zfs_service_up == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary:
            "Service {{ $labels.service }} is down on {{ $labels.instance }}"

      - alert: ZfsNfsSharesWithoutService
        expr: |
          (count(zfs_dataset_share_nfs == 1) > 0)
            and
          (zfs_service_up{service="nfs"} == 0)
        for: 2m
        labels:
          severity: critical
        annotations:
          summary:
            "NFS shares configured but NFS service is down on {{
            $labels.instance }}"

      - alert: ZfsSmbSharesWithoutService
        expr: |
          (count(zfs_dataset_share_smb == 1) > 0)
            and
          (zfs_service_up{service="smb"} == 0)
        for: 2m
        labels:
          severity: critical
        annotations:
          summary:
            "SMB shares configured but SMB service is down on {{
            $labels.instance }}"

      # --- Anomaly detection (uses recording rules below) ---

      - alert: ZfsDatasetAbnormalGrowth
        expr: |
          (
            (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg7d)
              > 2 * zfs:dataset_used_bytes:stddev7d
          )
          and
          (
            (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg7d)
              > max(1073741824, 0.1 * zfs:dataset_used_bytes:avg7d)
          )
        for: 1h
        labels:
          severity: warning
        annotations:
          summary:
            "Dataset {{ $labels.dataset }} usage is outside normal 7-day range"
          description:
            "Current usage has deviated more than 2 standard deviations from the
            7-day average and exceeds the minimum threshold floor."

      - alert: ZfsDatasetAbnormalGrowthShortTerm
        expr: |
          (
            (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg1d)
              > 3 * zfs:dataset_used_bytes:stddev1d
          )
          and
          (
            (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg1d)
              > max(1073741824, 0.1 * zfs:dataset_used_bytes:avg1d)
          )
        for: 30m
        labels:
          severity: warning
        annotations:
          summary:
            "Dataset {{ $labels.dataset }} usage spiking beyond 1-day baseline"
          description:
            "Current usage has deviated more than 3 standard deviations from the
            1-day average and exceeds the minimum threshold floor."

      - alert: ZfsPoolPredictedFull7d
        expr: predict_linear(zfs_pool_free_bytes[7d], 7 * 24 * 3600) < 0
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Pool {{ $labels.pool }} predicted to fill within 7 days"
          description:
            "Based on 7-day growth trend, pool {{ $labels.pool }} will run out
            of space."

      - alert: ZfsPoolPredictedFull1d
        expr: predict_linear(zfs_pool_free_bytes[1d], 24 * 3600) < 0
        for: 30m
        labels:
          severity: critical
        annotations:
          summary: "Pool {{ $labels.pool }} predicted to fill within 24 hours"
          description:
            "Based on 1-day growth trend, pool {{ $labels.pool }} will run out
            of space imminently."
```

### Recording Rules (`contrib/prometheus/recording_rules.yml`)

Recording rules pre-compute the baselines used by anomaly detection alerts.
These must be loaded into Prometheus alongside the alert rules.

```yaml
groups:
  - name: zfs_anomaly_baselines
    interval: 5m
    rules:
      # 1-day baselines (short-term anomaly detection)
      - record: zfs:dataset_used_bytes:avg1d
        expr: avg_over_time(zfs_dataset_used_bytes[1d])

      - record: zfs:dataset_used_bytes:stddev1d
        expr: stddev_over_time(zfs_dataset_used_bytes[1d])

      # 7-day baselines (weekly pattern anomaly detection)
      - record: zfs:dataset_used_bytes:avg7d
        expr: avg_over_time(zfs_dataset_used_bytes[7d])

      - record: zfs:dataset_used_bytes:stddev7d
        expr: stddev_over_time(zfs_dataset_used_bytes[7d])

      # Growth rate (bytes per second, smoothed over 1 hour)
      - record: zfs:dataset_used_bytes:deriv1h
        expr: deriv(zfs_dataset_used_bytes[1h])
```

**How it works:**

- `avg7d` / `stddev7d` capture the normal weekly pattern for each dataset
- `avg1d` / `stddev1d` capture the normal daily pattern (tighter window)
- The 7-day alert uses a 2-sigma threshold (catches ~5% of outliers in a normal
  distribution) with a 1-hour `for` duration to avoid transient spikes
- The 1-day alert uses a 3-sigma threshold (tighter band) with 30-minute `for`
  to catch sharp spikes quickly
- `predict_linear` on pool free bytes uses the actual time series slope to
  estimate when a pool will hit zero, giving operators lead time
- `deriv1h` provides a smoothed growth rate for Grafana dashboard panels

**Tuning notes for operators:**

- If alerts are too noisy, increase the sigma multiplier (e.g. 3 -> 4 for 7d)
- If alerts are too quiet, decrease the `for` duration or sigma multiplier
- The recording rules require Prometheus to have at least 7 days of history for
  the 7-day baselines to stabilize. Alerts will not fire meaningfully until then
- Anomaly alerts include a minimum threshold floor: deviations must exceed
  `max(1 GiB, 10% of baseline average)`. This prevents false positives on
  datasets with near-zero standard deviation (e.g. rarely-changing datasets
  where even a few MB would exceed 2-sigma)

---

## Testing Strategy

Following the GUIDE.md principle of testing real code paths with fake data
sources (no interface mocking, no mockery).

### Parser Tests (`pkg/zfs/`)

Table-driven tests for `parsePools`, `parseDatasets`, and `parseScanStatuses`
with string fixtures:

- Happy path with realistic multi-line output
- Single pool/dataset
- Fragmentation as `-` (NaN case)
- Malformed lines (wrong field count, non-numeric values)
- Empty output (no pools/datasets)
- ReadOnly `on` vs `off`
- `sharenfs` variations: `off`, `on`, NFS options string, `-` (volume)
- `sharesmb` variations: `off`, `on`, `-` (volume)
- Scan: no scan requested, scrub in progress, resilver in progress, completed
  scrub, multiple pools with different scan states
- Progress percentage extraction (e.g. `48.36%` -> `0.4836`)

### Client Tests (`pkg/zfs/`)

Inject a `Runner` that returns fixture data or errors:

```go
func TestClient_GetPools_Success(t *testing.T) {
    runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
        return []byte("tank\t10737418240\t5368709120\t5368709120\t33\t50\t1.00\tONLINE\toff\n"), nil
    }
    client := zfs.NewClient(runner, slog.Default())
    pools, err := client.GetPools(context.Background())
    // assert...
}
```

Test cases:

- Success with valid output
- Command not found (exec error)
- Command returns non-zero exit code
- Empty stdout

### Service Checker Tests (`pkg/host/`)

Inject a `Runner` that simulates `systemctl is-active` responses:

- Service active (stdout: `active\n`, exit 0)
- Service inactive (stdout: `inactive\n`, exit non-zero)
- Service failed (stdout: `failed\n`, exit non-zero)
- Unit not found (stderr: unit not found, exit non-zero)
- Fallback unit resolution (first unit missing, second exists)

### Collector Tests (`collector/`)

Use `testutil.CollectAndCompare` with a Client wired to a fake Runner:

```go
func TestCollector_PoolMetrics(t *testing.T) {
    runner := newFixtureRunner(poolOutput, datasetOutput, serviceResponses)
    client := zfs.NewClient(runner, newTestLogger())
    svcChecker := host.NewServiceChecker(runner, newTestLogger())
    coll := collector.NewCollector(client, svcChecker, newTestLogger())

    expected := `
        # HELP zfs_pool_size_bytes Total size of the ZFS pool in bytes.
        # TYPE zfs_pool_size_bytes gauge
        zfs_pool_size_bytes{pool="tank"} 1.073741824e+10
    `
    err := testutil.CollectAndCompare(coll, strings.NewReader(expected), "zfs_pool_size_bytes")
    // ...
}
```

Test scenarios from GUIDE.md checklist:

- Happy path: real-world data, verify metric values and labels
- Pool command failure: `up=0`, no pool/dataset/scan/service metrics
- Dataset command failure: `up=1`, pool + scan metrics present, no dataset
  metrics, service metrics still emitted
- Scan status failure: `up=1`, pool and dataset metrics present, no scan metrics
- Service check failure: `up=1`, pool/dataset/scan metrics present, no service
  metrics
- Health state-set: verify exactly one state is 1, rest are 0
- Scan metrics: resilver active with progress, scrub active, no scan active
- Share metrics: NFS on, SMB off, volume with `-` values
- Service up/down: verify `zfs_service_up` values
- Metric count: correct number of descriptors and emitted metrics

---

## Implementation Order

Each step should be a reviewable unit with passing tests:

1. **`pkg/zfs/` types and pool/dataset parsing** - Pool/Dataset types (including
   share fields), `parsePools`, `parseDatasets`, parser tests
2. **`pkg/zfs/` scan status parsing** - ScanStatus type, `parseScanStatuses`,
   scan parser tests
3. **`pkg/zfs/` client** - Client struct, Runner, DefaultRunner, GetPools,
   GetDatasets, GetScanStatuses, binary path acceptance, client tests
4. **`pkg/host/` service checker** - ServiceChecker, CheckServices, unit
   fallback logic, configurable service list, tests
5. **`config/`** - Config struct, flags (including `--zfs.zpool-path`,
   `--zfs.zfs-path`, `--host.services`), env overrides, binary path validation
   at startup, sentinel errors
6. **`collector/`** - Collector with Describe/Collect, pool metrics, dataset
   metrics (including share gauges), health state-set, scan metrics, service
   metrics, collector tests
7. **`exporter/`** - Landing page handler
8. **`cmd/zfs_exporter/main.go`** - Wire everything, HTTP server, graceful
   shutdown
9. **`contrib/prometheus/alerts.yml`** - Alert rules (exporter health, drive
   failure/rebuild, pool capacity, services, share mismatches)
10. **`contrib/grafana/zfs-status.json`** - Status dashboard (stat panels,
    alert-tied thresholds, quick-glance)
11. **`contrib/grafana/zfs-details.json`** - Details dashboard (all graphs,
    tables, time series for pool capacity, dataset usage, shares, services)
12. **`contrib/prometheus/recording_rules.yml`** - Anomaly detection recording
    rules (1-day and 7-day baselines)
13. **Anomaly detection alerts** - Add anomaly alerts to `alerts.yml` (abnormal
    growth with `max(1 GiB, 10%)` floor, predicted full)
14. **`contrib/grafana/zfs-combined.json`** - Combined dashboard (status panels
    at top + expandable drill-down rows including anomaly detection)
15. **Integration smoke test** - Manual verification on a ZFS host

---

## Error Handling

| Scenario                                     | Behavior                                                                     |
| -------------------------------------------- | ---------------------------------------------------------------------------- |
| `zpool` binary not found                     | `up=0`, log error, return (no metrics emitted except up and scrape_duration) |
| `zpool list` returns non-zero                | `up=0`, log error with stderr, return                                        |
| `zpool list` returns empty output            | `up=1`, zero pool metrics (valid: host has no pools)                         |
| `zfs list` fails                             | `up=1`, log warning, emit pool metrics only (no dataset metrics)             |
| `zfs list` returns empty output              | `up=1`, pool metrics + zero dataset metrics                                  |
| `zpool status` fails                         | `up=1`, log warning, emit pool and dataset metrics without scan metrics      |
| `zpool status` output unparseable for a pool | Skip that pool's scan metrics, log warning                                   |
| `systemctl` fails for a service              | Log warning, skip that service, continue with others                         |
| Systemd unit does not exist                  | Silently skip (service not installed on this host)                           |
| Unparseable line in output                   | Skip line, log warning, continue with remaining lines                        |
| Fragmentation is `-`                         | Emit NaN for `zfs_pool_fragmentation_ratio`                                  |
| Share property is `-` (volumes)              | Emit 0 for `zfs_dataset_share_nfs` and `zfs_dataset_share_smb`               |
| Scrape timeout exceeded                      | Context cancellation kills running commands, `up=0`                          |

---

## Permissions

`zpool list` and `zfs list` are readable by any user on standard OpenZFS
installations. `systemctl is-active` is readable by any user. The exporter does
not need root privileges for V1 metrics.

The systemd service (from goreleaser Debian packaging) already runs as a
dedicated `zfs_exporter` user with `NoNewPrivileges=yes`.

If the system restricts ZFS command access, the operator can add the service
user to the appropriate group (typically `zfs` or configure ZFS delegation via
`zfs allow`).

---

## Out of Scope for V1

These are candidates for future versions:

- **ARC stats** (`/proc/spl/kstat/zfs/arcstats`) - Requires procfs parsing,
  Linux-only
- **I/O stats** (`zpool iostat`) - Latency histograms, throughput
- **Per-vdev error counts** (`zpool status` vdev tree parsing) - Read/write/
  checksum errors per vdev, complex indentation-based tree parsing
- **Snapshot metrics** - Can be noisy with many snapshots; pdf/zfs_exporter
  disables this by default for the same reason
- **Dataset properties** (compression ratio, quota, reservation) - Easy to add
  via additional `-o` columns
- **Configurable collectors** (enable/disable pool, dataset, service collection
  via flags)
- **Port reachability checks** - Verify NFS (2049), SMB (445), iSCSI (3260)
  ports are actually listening, beyond just systemd unit status
- **iSCSI target/LUN enumeration** - Discover targets via
  `/sys/kernel/config/target/` or `targetcli`
- **macOS testing** - Builds will target darwin but testing is Linux-focused
- **Anomaly detection: seasonality** - Week-over-week comparison for datasets
  with weekly usage patterns (e.g. backup datasets that grow on weekends)

---

## Resolved Decisions

These questions were raised during planning and resolved before implementation.

1. **Per-vdev error counts:** Deferred to V2. Documented in Out of Scope as a
   future feature. Parsing the indentation-based vdev tree is significantly more
   complex than scan line parsing and not required for V1 goals.

2. **Service check: configured list.** The exporter checks a configured list of
   services, specified via CLI flag (`--host.services`) or config file. Default
   list: `zfs`, `nfs`, `smb`, `iscsi`. Operators can add or remove services as
   needed. No auto-discovery in V1.

3. **Scrape timeout: total budget.** A single context with deadline covers the
   entire `Collect()` call (all commands + service checks). If this proves
   insufficient, per-command budgets can be added later.

4. **Binary paths: exposed as flags with validation.** `--zfs.zpool-path` and
   `--zfs.zfs-path` flags default to `zpool` and `zfs` (found via PATH). At
   startup, the exporter validates that the configured paths exist and are
   executable. If validation fails, the exporter logs an error and exits. This
   applies to both default and non-standard paths.

5. **Dashboards: three dashboards.** See the Grafana Dashboards section for
   details on all three: Status (glance), Details (graphs), Combined
   (status panels + expandable drill-down).

6. **Anomaly detection minimum threshold: `max(1 GiB, 10% of avg)`.** Anomaly
   alerts include a floor condition so datasets with near-zero stddev don't fire
   on trivial changes. The deviation must exceed both 1 GiB and 10% of the
   baseline average, whichever is larger.
