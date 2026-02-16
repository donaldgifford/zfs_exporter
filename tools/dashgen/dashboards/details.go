package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
)

// DetailsConfig holds the parameters needed to build the details dashboard.
type DetailsConfig struct {
	Services []panels.ServiceConfig
}

// BuildDetails creates the ZFS Details dashboard — expanded rows with
// drill-down graphs and tables for pools, datasets, services, and anomalies.
func BuildDetails(cfg DetailsConfig) (*dashboard.DashboardBuilder, error) {
	b := dashboard.NewDashboardBuilder("ZFS Details").
		Uid("zfs-details").
		Tags([]string{"zfs", "prometheus"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone("browser").
		Editable().
		Tooltip(dashboard.DashboardCursorSyncCrosshair)

	b = b.WithVariable(datasourceVar()).
		WithVariable(poolVar())

	// Row: Pool Capacity (expanded, panels as siblings).
	b = b.WithRow(dashboard.NewRowBuilder("Pool Capacity")).
		WithPanel(panels.PoolUsageOverTime().Span(10)).
		WithPanel(panels.PoolUsageBars()).
		WithPanel(panels.Fragmentation())

	// Row: Dataset Usage (expanded, panels as siblings).
	b = b.WithRow(dashboard.NewRowBuilder("Dataset Usage")).
		WithPanel(panels.TopDatasets()).
		WithPanel(panels.AvailableSpace()).
		WithPanel(panels.DatasetUsageOverTime())

	// Per-service rows (collapsed, panels nested inside row).
	for _, svc := range cfg.Services {
		b = b.WithRow(serviceRow(svc))
	}

	// Row: Anomaly Detection (expanded, panels as siblings).
	b = b.WithRow(dashboard.NewRowBuilder("Anomaly Detection")).
		WithPanel(panels.GrowthRate()).
		WithPanel(panels.DeviationTable()).
		WithPanel(panels.PoolFillPrediction())

	return b, nil
}

// serviceRow returns a collapsed row containing the panels for a single service.
func serviceRow(svc panels.ServiceConfig) *dashboard.RowBuilder {
	return dashboard.NewRowBuilder(svc.Label).
		WithPanel(panels.ServiceStat(svc)).
		WithPanel(panels.ShareTable(svc)).
		WithPanel(panels.ServiceTimeline(svc))
}
