// Package dashboards provides functions that build complete Grafana dashboard
// definitions using the Foundation SDK. Each function returns a configured
// DashboardBuilder ready to be built and serialized to JSON.
package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
)

// StatusConfig holds the parameters needed to build the status dashboard.
type StatusConfig struct {
	Services []panels.ServiceConfig
}

// BuildStatus creates the ZFS Status dashboard — a NOC-screen overview with
// stat panels for pool health and service status.
func BuildStatus(cfg StatusConfig) (*dashboard.DashboardBuilder, error) {
	b := dashboard.NewDashboardBuilder("ZFS Status").
		Uid("zfs-status").
		Tags([]string{"zfs", "prometheus"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone("browser").
		Editable().
		Tooltip(dashboard.DashboardCursorSyncCrosshair)

	// Variables: datasource + pool.
	b = b.WithVariable(datasourceVar()).
		WithVariable(poolVar())

	// Row: Pool Health.
	b = b.WithRow(dashboard.NewRowBuilder("Pool Health")).
		WithPanel(panels.PoolHealth()).
		WithPanel(panels.PoolCapacity()).
		WithPanel(panels.ResilverScrub()).
		WithPanel(panels.DaysUntilFull())

	// Row: Service Health.
	b = b.WithRow(dashboard.NewRowBuilder("Service Health")).
		WithPanel(panels.ServiceStatusAll())

	// Per-service mismatch panels (only for services with ShareMetric).
	for _, svc := range cfg.Services {
		if svc.ShareMetric == "" {
			continue
		}
		b = b.WithPanel(panels.ShareMismatch(svc))
	}

	b = b.WithPanel(panels.ExporterUp())

	return b, nil
}

// datasourceVar returns the common "datasource" template variable.
func datasourceVar() *dashboard.DatasourceVariableBuilder {
	return dashboard.NewDatasourceVariableBuilder("datasource").
		Label("Data Source").
		Type("prometheus")
}

// poolVar returns the common "pool" template variable.
func poolVar() *dashboard.QueryVariableBuilder {
	return dashboard.NewQueryVariableBuilder("pool").
		Label("Pool").
		Datasource(panels.DSRef()).
		Query(dashboard.StringOrMap{String: cog.ToPtr("label_values(zfs_pool_size_bytes, pool)")}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		Sort(dashboard.VariableSortAlphabeticalAsc).
		Multi(true).
		IncludeAll(true)
}
