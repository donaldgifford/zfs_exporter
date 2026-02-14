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

	// Top stat panels (no row header).
	b = b.WithPanel(panels.PoolHealth()).
		WithPanel(panels.PoolCapacity()).
		WithPanel(panels.ServiceStatusAll()).
		WithPanel(panels.ResilverScrub()).
		WithPanel(panels.DaysUntilFull()).
		WithPanel(panels.ExporterUp())

	// Pool Details (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Pool Details").
			WithPanel(panels.PoolUsageOverTime()).
			WithPanel(panels.PoolUsageBars()).
			WithPanel(panels.Fragmentation()),
	)

	// Dataset Details (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Dataset Details").
			WithPanel(panels.TopDatasets()).
			WithPanel(panels.AvailableSpace()).
			WithPanel(panels.DatasetUsageOverTime()),
	)

	// Per-service rows (collapsed).
	for _, svc := range cfg.Services {
		b = b.WithRow(serviceRow(svc))
	}

	// Anomaly Detection (collapsed row).
	b = b.WithRow(
		dashboard.NewRowBuilder("Anomaly Detection").
			WithPanel(panels.GrowthRate()).
			WithPanel(panels.DeviationTable()).
			WithPanel(panels.PoolFillPrediction()),
	)

	return b, nil
}
