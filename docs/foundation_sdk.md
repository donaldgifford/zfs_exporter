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
- No validation of PromQL expressions or panel references

## Proposed Solution

Build a Go tool in `tools/dashgen/` that uses the
[Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk) to
produce dashboard JSON from a Go config struct. The config declares which
services to include, and the tool generates only the panels and rows for those
services. Run via `go generate`.

This is the **v1 approach**: config lives in Go, `go generate` produces JSON,
generated files are committed. Fast to build and test. See the
[Future: CLI Scaffolding](#future-cli-scaffolding-kubebuilder-pattern) section
for the planned evolution into a kubebuilder-style CLI tool.

```
tools/dashgen/
  main.go           # entrypoint: reads config, calls builders, writes JSON
  config.go         # Config struct, defaults, validation
  panels/           # reusable panel builder functions
    pool.go         # pool health, capacity, fragmentation panels
    dataset.go      # dataset usage, available space panels
    service.go      # service status, timeline, share/zvol tables
    anomaly.go      # growth rate, deviation, fill prediction panels
    status.go       # top-level stat panels (exporter up, mismatch, etc.)
    helpers.go      # shared builder utilities (datasource ref, thresholds)
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

The config is a Go struct in `tools/dashgen/config.go`. For now, changing
services means editing this struct and running `go generate`. This is the
fastest path to a working tool.

```go
type Config struct {
    // Services to include in dashboards. Each entry generates service-specific
    // panels (status stat, detail table, timeline). Only listed services appear.
    Services []ServiceConfig

    // Dashboards to generate. Defaults to all three.
    Dashboards DashboardSet

    // OutputDir is the directory to write JSON files.
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

Default config matches current behavior (all services, all dashboards):

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

A user who only runs NFS and iSCSI edits the config:

```go
var DefaultConfig = Config{
    Services: []ServiceConfig{
        {Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
        {Key: "iscsi", Label: "iSCSI", UseZvols: true},
    },
    // ...
}
```

Then runs `go generate ./...`. The generated dashboards have no SMB panels at
all -- no hidden rows, no empty panels, no "No data" messages.

## go:generate Integration

Add a generate directive in a top-level file (e.g. `generate.go`):

```go
//go:generate go run ./tools/dashgen
```

Running `go generate ./...` rebuilds all dashboard JSON files using the config
in `tools/dashgen/config.go`. The generated files are committed to the repo so
users get working dashboards out of the box with the default config.

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
| Per-service (collapsed) | Service Stat, Share/Zvol Table, Service Timeline |
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

## Future: CLI Scaffolding (kubebuilder pattern)

The v1 approach (Go struct config + `go generate`) gets us a working tool fast.
The next evolution follows the kubebuilder pattern:

1. **`dashgen init`** -- scaffolds out a project structure with a default config
   file and a `generate.go` directive, similar to `kubebuilder init` creating a
   project skeleton

2. **`dashgen add service <key>`** -- adds a new service entry to the config
   with sensible defaults, similar to `kubebuilder create api` adding a new
   resource type

3. **`make dashboards`** / **`go generate`** -- reads the scaffolded config and
   generates dashboard JSON, same as `make generate` in kubebuilder running
   controller-gen on the annotated types

The progression:

```
v1 (now):     edit Go struct  -->  go generate  -->  dashboard JSON
v2 (future):  dashgen init    -->  edit config  -->  go generate  -->  dashboard JSON
              dashgen add svc
```

In v2, the config file format (YAML, JSON, or even annotated Go) becomes the
natural interface. The CLI handles scaffolding and validation, `go generate`
does the actual codegen. The tool could live in its own module
(`tools/dashgen/go.mod`) to keep dependencies separate, making it easy to
extract into a standalone repo later.

Key decisions deferred to v2:
- Config file format (YAML with `gopkg.in/yaml.v3`, or stick with Go)
- Whether to generate Prometheus recording rules and alert rules from the same
  config
- Plugin system for custom panel types beyond the built-in set
- Whether the CLI binary gets distributed independently (homebrew, go install)

## File Changes Summary

New files:
- `generate.go` (go:generate directive)
- `tools/dashgen/main.go`
- `tools/dashgen/config.go`
- `tools/dashgen/panels/pool.go`
- `tools/dashgen/panels/dataset.go`
- `tools/dashgen/panels/service.go`
- `tools/dashgen/panels/anomaly.go`
- `tools/dashgen/panels/status.go`
- `tools/dashgen/panels/helpers.go` (shared builder utilities)
- `tools/dashgen/dashboards/status.go`
- `tools/dashgen/dashboards/details.go`
- `tools/dashgen/dashboards/combined.go`

Modified files:
- `go.mod` / `go.sum` (new dependency: grafana-foundation-sdk)
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
   metrics and services. Service-specific alerts (e.g. NFS share mismatch)
   could be conditionally generated from the same config.

4. **Separate Go module:** Should `tools/dashgen/` have its own `go.mod` from
   the start to keep the Foundation SDK dependency out of the main exporter
   module? This would also make it easier to extract into a standalone repo
   later.
