# Grafana Dashboard Code Generation with Foundation SDK

## Problem

The current hand-maintained JSON dashboards have recurring issues with empty
panels and rows for services that aren't configured. We've worked around this
with Grafana template variables and row repeat, but the approach is fragile:

- Dashboard JSON is ~2600 lines per file, hard to review in PRs
- Service-specific sections exist even when the user doesn't use that service
- Grafana row repeat is a workaround, not a real solution -- rows still exist in
  the JSON, just hidden at render time
- Adding a new service type means editing 3 JSON files by hand
- No compile-time validation of PromQL expressions or panel references

## Proposed Solution

Build a Go code generator in `tools/dashgen/` that uses the
[Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk) to
produce dashboard JSON from a configuration struct. Run it via `go generate` to
write the 3 JSON files into `contrib/grafana/`.

```
tools/dashgen/
  main.go           # entrypoint: reads config, calls builders, writes JSON
  config.go         # Config struct and defaults
  panels/           # reusable panel builder functions
    pool.go         # pool health, capacity, fragmentation panels
    dataset.go      # dataset usage, available space panels
    service.go      # service status, timeline, share/zvol tables
    anomaly.go      # growth rate, deviation, fill prediction panels
    status.go       # top-level stat panels (exporter up, mismatch, etc.)
  dashboards/
    status.go       # builds zfs-status.json
    details.go      # builds zfs-details.json
    combined.go     # builds zfs-combined.json
```

## Grafana Foundation SDK

**Module:** `github.com/grafana/grafana-foundation-sdk/go` (v0.0.7, targets
Grafana >= 12.0, public preview status)

The SDK provides typed Go builders for every Grafana panel type (stat,
timeseries, table, bargauge), template variables, rows, transformations, and
thresholds. Dashboards are built with a fluent builder pattern and serialized
with standard `encoding/json`.

Key packages:
- `dashboard` -- dashboard, row, variable, threshold builders
- `stat`, `timeseries`, `table`, `bargauge` -- typed panel builders
- `prometheus` -- Prometheus query builder
- `common` -- shared enums (color modes, orientations, display modes)
- `cog` -- utilities (`ToPtr[T]()`, Builder interface)

### Example

```go
builder := dashboard.NewDashboardBuilder("ZFS Status").
    Uid("zfs-status").
    Tags([]string{"zfs", "prometheus"}).
    Refresh("30s").
    WithVariable(dashboard.NewDatasourceVariableBuilder("datasource").
        Label("Data Source").
        Type("prometheus")).
    WithVariable(dashboard.NewQueryVariableBuilder("pool").
        Label("Pool").
        Query(dashboard.StringOrMap{String: cog.ToPtr("label_values(zfs_pool_size_bytes, pool)")}).
        Datasource(dsRef("$datasource")).
        Multi(true).
        IncludeAll(true)).
    WithRow(dashboard.NewRowBuilder("Pool Health")).
    WithPanel(poolHealthPanel())

dash, _ := builder.Build()
json.MarshalIndent(dash, "", "  ")
```

## Configuration

A Go struct defines which services and dashboard sections to generate:

```go
type Config struct {
    // Services to include in dashboards. Each entry generates service-specific
    // panels (status stat, detail table, timeline). Only listed services appear.
    Services []ServiceConfig

    // Dashboards to generate. Defaults to all three.
    Dashboards DashboardSet

    // OutputDir is the directory to write JSON files. Default: contrib/grafana/
    OutputDir string
}

type ServiceConfig struct {
    // Key is the service identifier used in metrics (e.g. "nfs", "smb", "iscsi").
    Key string

    // Label is the display name in dashboard panels (e.g. "NFS", "SMB", "iSCSI").
    Label string

    // ShareMetric is the metric name for share detection.
    // For NFS: "zfs_dataset_share_nfs", for SMB: "zfs_dataset_share_smb".
    // Empty string means no share metric (e.g. iSCSI uses zvols instead).
    ShareMetric string

    // UseZvols indicates this service should show zvol inventory instead of
    // share datasets (true for iSCSI).
    UseZvols bool
}

type DashboardSet struct {
    Status   bool // zfs-status.json
    Details  bool // zfs-details.json
    Combined bool // zfs-combined.json
}
```

Default config matches current behavior:

```go
var DefaultConfig = Config{
    Services: []ServiceConfig{
        {Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
        {Key: "smb", Label: "SMB", ShareMetric: "zfs_dataset_share_smb"},
        {Key: "iscsi", Label: "iSCSI", UseZvols: true},
    },
    Dashboards: DashboardSet{Status: true, Details: true, Combined: true},
    OutputDir:  "contrib/grafana",
}
```

A user who only runs NFS and iSCSI would configure:

```go
Services: []ServiceConfig{
    {Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
    {Key: "iscsi", Label: "iSCSI", UseZvols: true},
}
```

The generated dashboards would have no SMB panels at all -- no hidden rows,
no empty panels, no "No data" messages.

## go:generate Integration

Add a generate directive in a top-level file (e.g. `generate.go`):

```go
//go:generate go run ./tools/dashgen
```

Running `go generate ./...` rebuilds all dashboard JSON files. The generated
files are committed to the repo so users don't need to run the generator -- they
get working dashboards out of the box with the default config.

The Makefile gets a new target:

```makefile
dashboards:  ## Regenerate Grafana dashboard JSON
    go generate ./generate.go
```

## Panel Inventory (What Gets Generated)

The generator must reproduce all panels from the current dashboards. Here's what
each dashboard contains:

### zfs-status.json (8 panels)

Top-level stat panels only (NOC screen):

| Section | Panels |
|---------|--------|
| Pool Health | Pool Health, Pool Capacity, Resilver/Scrub, Days Until Full |
| Service Health | Service Status, NFS Mismatch*, SMB Mismatch*, Exporter Up |

*Mismatch panels are per-service and only generated for services with a
ShareMetric.

### zfs-details.json (18+ panels)

Expanded rows with drill-down:

| Row | Panels |
|-----|--------|
| Pool Capacity | Pool Usage Over Time, Pool Usage Bar, Fragmentation |
| Dataset Usage | Top Datasets, Available Space, Usage Over Time |
| Per-service (repeated) | Service Stat, Share/Zvol Table, Service Timeline |
| Anomaly Detection | Growth Rate, 7d Deviation Table, Pool Fill Prediction |

### zfs-combined.json (21+ panels)

Status panels at top, collapsed drill-down rows:

| Section | Panels |
|---------|--------|
| Top Stats | Pool Health, Capacity %, Service Status, Resilver/Scrub, Days Until Full, Exporter Up |
| Pool Details (collapsed) | Usage Over Time, Usage Bars, Fragmentation |
| Dataset Details (collapsed) | Top Datasets, Available Space, Usage Over Time |
| Per-service (collapsed, repeated) | Service Stat, Share/Zvol Table, Service Timeline |
| Anomaly Detection (collapsed) | Growth Rate, 7d Deviation, Pool Fill Prediction |

## Unique Metrics Referenced

All PromQL expressions use these metrics:

**Pool:** `zfs_pool_health`, `zfs_pool_allocated_bytes`, `zfs_pool_size_bytes`,
`zfs_pool_free_bytes`, `zfs_pool_resilver_active`, `zfs_pool_scrub_active`,
`zfs_pool_fragmentation_ratio`

**Dataset:** `zfs_dataset_used_bytes`, `zfs_dataset_available_bytes`,
`zfs_dataset_share_nfs`, `zfs_dataset_share_smb`

**Service:** `zfs_service_up`

**Recording Rules:** `zfs:dataset_used_bytes:avg7d`,
`zfs:dataset_used_bytes:stddev7d`

**Exporter:** `zfs_up`

## Reusable Panel Builders

The panel builder functions in `panels/` are the core of the generator. Each
returns a `cog.Builder[dashboard.Panel]` that can be added to any dashboard.

```go
// panels/pool.go
func PoolHealth(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func PoolCapacity(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func PoolUsageOverTime(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func PoolUsageBars(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func Fragmentation(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func ResilverScrub(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func DaysUntilFull(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]

// panels/dataset.go
func TopDatasets(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func AvailableSpace(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func DatasetUsageOverTime(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]

// panels/service.go
func ServiceStatusAll(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func ServiceStat(ds dashboard.DataSourceRef, svc ServiceConfig) cog.Builder[dashboard.Panel]
func ServiceTimeline(ds dashboard.DataSourceRef, svc ServiceConfig) cog.Builder[dashboard.Panel]
func ShareTable(ds dashboard.DataSourceRef, svc ServiceConfig) cog.Builder[dashboard.Panel]
func ShareMismatch(ds dashboard.DataSourceRef, svc ServiceConfig) cog.Builder[dashboard.Panel]
func ExporterUp(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]

// panels/anomaly.go
func GrowthRate(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func DeviationTable(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
func PoolFillPrediction(ds dashboard.DataSourceRef) cog.Builder[dashboard.Panel]
```

Service panels are parameterized by `ServiceConfig`, so the same builder
produces NFS, SMB, or iSCSI panels depending on the config. The `ShareTable`
function switches between a share-dataset query (`zfs_dataset_share_nfs == 1`)
and a zvol query (`zfs_dataset_used_bytes{type="volume"}`) based on `UseZvols`.

## Options Considered

### Option A: Config embedded in Go (recommended)

The default config lives in `tools/dashgen/config.go` as a Go struct. Users who
want different services edit the struct and run `go generate`. Generated JSON
is committed.

**Pros:**
- Type-safe, compile-time errors for bad config
- No extra file format to maintain
- `go generate` is idiomatic Go
- Generated JSON is committed, no runtime dependency on the tool

**Cons:**
- Changing services requires editing Go code (minor -- it's a simple struct)
- Users who don't write Go must edit a Go file (but it's trivial)

### Option B: External YAML config file

A `dashgen.yaml` file at the repo root defines services. The tool reads it at
generate time.

```yaml
services:
  - key: nfs
    label: NFS
    share_metric: zfs_dataset_share_nfs
  - key: iscsi
    label: iSCSI
    use_zvols: true
output_dir: contrib/grafana
```

**Pros:**
- No Go knowledge needed to change config
- Could be useful if dashboards are generated in CI for different deployments

**Cons:**
- Extra file to maintain
- Loses compile-time validation
- YAML parsing adds a dependency (or use `encoding/json`)

### Option C: CLI flags

The tool accepts flags for service selection:

```bash
go run ./tools/dashgen --services=nfs,iscsi --output=contrib/grafana
```

**Pros:**
- Flexible, no config file needed
- Easy to script for different deployments

**Cons:**
- `go generate` directives with flags are harder to read
- Can't express complex per-service config (share metrics, zvols) via flags
  without making the interface unwieldy

### Recommendation

**Option A** for the default path (config in Go, `go generate`, committed JSON).

Optionally support Option C flags as overrides for one-off generation. The flags
would only need `--services` since the service definitions (share metric, zvols)
are well-known and can be looked up from the key.

## Implementation Plan

### Phase 1: Scaffold and core panels

1. Add `github.com/grafana/grafana-foundation-sdk/go` dependency
2. Create `tools/dashgen/` directory structure
3. Implement `Config` struct and defaults
4. Implement pool panel builders (`panels/pool.go`)
5. Implement dataset panel builders (`panels/dataset.go`)
6. Build `zfs-status.json` generator as proof of concept
7. Validate output matches current JSON (diff test)

### Phase 2: Service panels and remaining dashboards

1. Implement service panel builders (`panels/service.go`)
2. Implement anomaly panel builders (`panels/anomaly.go`)
3. Build `zfs-details.json` generator
4. Build `zfs-combined.json` generator
5. Add `//go:generate` directive
6. Add `make dashboards` target

### Phase 3: Testing and validation

1. Add a test that generates all dashboards and validates JSON structure
2. Add a test that checks generated output matches committed files (staleness
   check)
3. Remove the hidden `svc_nfs/svc_smb/svc_iscsi` template variable workaround
   from generated dashboards (no longer needed -- panels simply aren't generated
   for unconfigured services)
4. Validate dashboards import cleanly in Grafana

### Phase 4: CI integration

1. Add CI step that runs `go generate` and checks for uncommitted changes
   (ensures generated files stay in sync with generator code)
2. Update CLAUDE.md and README with dashboard generation instructions

## File Changes Summary

New files:
- `tools/dashgen/main.go`
- `tools/dashgen/config.go`
- `tools/dashgen/panels/pool.go`
- `tools/dashgen/panels/dataset.go`
- `tools/dashgen/panels/service.go`
- `tools/dashgen/panels/anomaly.go`
- `tools/dashgen/panels/status.go`
- `tools/dashgen/panels/helpers.go` (shared builder utilities)
- `dashboards/status.go`
- `dashboards/details.go`
- `dashboards/combined.go`
- `generate.go` (go:generate directive)

Modified files:
- `go.mod` / `go.sum` (new dependency)
- `Makefile` (new `dashboards` target)
- `contrib/grafana/*.json` (regenerated output)

## Open Questions

1. **Grafana version targeting:** The SDK v0.0.7 targets Grafana >= 12.0. Our
   dashboards currently declare `schemaVersion: 39` and `pluginVersion: 10.0.0`.
   Need to verify the SDK output is compatible with Grafana 10.x+ or if we need
   an older SDK branch.

2. **Recording rules:** Should the generator also produce the Prometheus
   recording rules in `contrib/prometheus/`? They reference the same metric
   names and could stay in sync.

3. **Alert rules:** Same question for alert rules -- they reference the same
   metrics and services. Could be generated from the same config.

4. **Custom services:** Should we support user-defined services beyond
   nfs/smb/iscsi? The `ServiceConfig` struct already supports this, but we'd
   need to define what metrics a custom service uses.
