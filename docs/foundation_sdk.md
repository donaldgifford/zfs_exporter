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
produce dashboard JSON from a YAML config file. The tool reads a `dashgen.yaml`
that declares which services to include, then generates only the panels and rows
for those services. Run it via `go generate` or directly as a standalone binary.

```
tools/dashgen/
  main.go           # entrypoint: reads YAML, calls builders, writes JSON
  config.go         # Config struct, YAML parsing, validation
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
dashgen.yaml        # default config (repo root, committed)
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

A YAML file (`dashgen.yaml`) defines which services and dashboard sections to
generate. The tool reads this at runtime, validates it, and reports clear errors
for any problems. No recompilation needed to change the config.

```yaml
# dashgen.yaml -- dashboard generation config
# Only services listed here get panels in the generated dashboards.

output_dir: contrib/grafana

dashboards:
  status: true    # zfs-status.json  (NOC screen, stat panels only)
  details: true   # zfs-details.json (expanded rows, full drill-down)
  combined: true  # zfs-combined.json (status + collapsed drill-down)

services:
  - key: nfs
    label: NFS
    share_metric: zfs_dataset_share_nfs

  - key: smb
    label: SMB
    share_metric: zfs_dataset_share_smb

  - key: iscsi
    label: iSCSI
    use_zvols: true
```

The Go structs that back this are straightforward:

```go
type Config struct {
    OutputDir  string         `yaml:"output_dir"`
    Dashboards DashboardSet   `yaml:"dashboards"`
    Services   []ServiceConfig `yaml:"services"`
}

type DashboardSet struct {
    Status   bool `yaml:"status"`
    Details  bool `yaml:"details"`
    Combined bool `yaml:"combined"`
}

type ServiceConfig struct {
    Key         string `yaml:"key"`
    Label       string `yaml:"label"`
    ShareMetric string `yaml:"share_metric"`
    UseZvols    bool   `yaml:"use_zvols"`
}
```

Validation happens at runtime with clear errors:

```
$ go run ./tools/dashgen --config dashgen.yaml
dashgen: error: service[0]: "key" is required
dashgen: error: service "nfs": must set either "share_metric" or "use_zvols"
```

A user who only runs NFS and iSCSI simply removes the SMB entry:

```yaml
services:
  - key: nfs
    label: NFS
    share_metric: zfs_dataset_share_nfs
  - key: iscsi
    label: iSCSI
    use_zvols: true
```

The generated dashboards have no SMB panels at all -- no hidden rows, no empty
panels, no "No data" messages. This also makes the tool portable: if it
eventually becomes its own project, the YAML config is the natural interface.

### Default Config

The repo ships a `dashgen.yaml` at the root with all services enabled. This is
the config `go generate` uses to produce the committed dashboard JSON. Users
clone the repo and get working dashboards without running the tool.

For custom deployments, copy `dashgen.yaml`, edit it, and run the tool:

```bash
go run ./tools/dashgen --config my-dashgen.yaml --output /path/to/grafana/dashboards
```

## go:generate Integration

Add a generate directive in a top-level file (e.g. `generate.go`):

```go
//go:generate go run ./tools/dashgen --config dashgen.yaml
```

Running `go generate ./...` reads `dashgen.yaml` from the repo root and
rebuilds all dashboard JSON files. The generated files are committed so users
get working dashboards out of the box with the default config.

The Makefile gets a new target:

```makefile
dashboards:  ## Regenerate Grafana dashboard JSON
    go generate ./generate.go
```

For custom deployments, users copy `dashgen.yaml`, edit it, and run the tool
directly without `go generate`:

```bash
cp dashgen.yaml my-dashgen.yaml
# edit my-dashgen.yaml to remove SMB, add custom services, etc.
go run ./tools/dashgen --config my-dashgen.yaml --output /my/grafana/dashboards
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

## CLI Interface

The tool accepts a config file path and optional output directory override:

```bash
# Default: reads dashgen.yaml from current directory
go run ./tools/dashgen

# Explicit config and output
go run ./tools/dashgen --config dashgen.yaml --output contrib/grafana

# Override output dir (useful for deploying to a Grafana provisioning path)
go run ./tools/dashgen --config dashgen.yaml --output /etc/grafana/dashboards
```

Flags:
- `--config` (`-c`): Path to YAML config file (default: `dashgen.yaml`)
- `--output` (`-o`): Override `output_dir` from config (optional)

### YAML Dependency

YAML parsing requires a dependency. Options:
- `gopkg.in/yaml.v3` -- standard, widely used, no transitive deps
- `github.com/goccy/go-yaml` -- faster, fewer allocations

Recommend `gopkg.in/yaml.v3` since it's the de facto standard and this is a
build-time tool where parse speed doesn't matter.

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
- `dashgen.yaml` (default config, repo root)
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
- `go.mod` / `go.sum` (new dependencies: grafana-foundation-sdk, yaml.v3)
- `Makefile` (new `dashboards` target)
- `contrib/grafana/*.json` (regenerated output)

## Open Questions

1. **Grafana version targeting:** The SDK v0.0.7 targets Grafana >= 12.0. Our
   dashboards currently declare `schemaVersion: 39` and `pluginVersion: 10.0.0`.
   Need to verify the SDK output is compatible with Grafana 10.x+ or if we need
   an older SDK branch.

2. **Recording rules:** Should the generator also produce the Prometheus
   recording rules in `contrib/prometheus/`? They reference the same metric
   names and could stay in sync with the YAML config.

3. **Alert rules:** Same question for alert rules -- they reference the same
   metrics and services. Service-specific alerts (e.g. NFS share mismatch)
   could be conditionally generated from the same config.

4. **Custom services:** The `ServiceConfig` struct already supports arbitrary
   services via YAML. Should we document how to add a custom service, or keep
   it to the well-known set (nfs, smb, iscsi)?

5. **Standalone tool:** If this becomes its own repo/binary, the YAML config
   is already the right interface. Should we structure `tools/dashgen/` as its
   own Go module from the start (`tools/dashgen/go.mod`) to keep its
   dependencies separate from the exporter?
