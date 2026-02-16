package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
)

// CombinedConfig holds the parameters needed to build the combined dashboard.
type CombinedConfig struct {
	Services []panels.ServiceConfig
}

// BuildCombined creates the ZFS Combined dashboard — status stat panels at the
// top with collapsed drill-down rows for pools, datasets, services, and anomalies.
func BuildCombined(cfg CombinedConfig) (*dashboard.DashboardBuilder, error) {
	b := dashboard.NewDashboardBuilder("ZFS Combined").
		Uid("zfs-combined").
		Tags([]string{"zfs", "prometheus"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone("browser").
		Editable().
		Tooltip(dashboard.DashboardCursorSyncCrosshair)

	b = b.WithVariable(datasourceVar()).
		WithVariable(poolVar())

	// Top stat panels (no row header): 6 across at w:4, h:4.
	b = b.WithPanel(panels.PoolHealth().Height(4).Span(4)).
		WithPanel(panels.PoolCapacity().Height(4).Span(4)).
		WithPanel(panels.ServiceStatusAll().Height(4).Span(4)).
		WithPanel(panels.ResilverScrub().Height(4).Span(4)).
		WithPanel(panels.DaysUntilFull().Height(4).Span(4)).
		WithPanel(panels.ExporterUp().Height(4).Span(4))

	// Pool Details (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Pool Details").
			WithPanel(panels.PoolUsageOverTime()).
			WithPanel(panels.PoolUsageBars()).
			WithPanel(panels.Fragmentation().Span(6)),
	)

	// Dataset Details (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Dataset Details").
			WithPanel(panels.TopDatasets().Height(8).Span(12)).
			WithPanel(panels.AvailableSpace().Height(8).Span(12)).
			WithPanel(panels.DatasetUsageOverTime().Height(8).Span(24)),
	)

	// Per-service rows (collapsed).
	for _, svc := range cfg.Services {
		b = b.WithRow(serviceRow(svc))
	}

	// Anomaly Detection (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Anomaly Detection").
			WithPanel(panels.GrowthRate().Height(8).Span(12)).
			WithPanel(panels.DeviationTable().Height(8).Span(12)).
			WithPanel(panels.PoolFillPrediction().Height(8).Span(24)),
	)

	return b, nil
}
