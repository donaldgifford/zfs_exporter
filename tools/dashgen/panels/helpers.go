// Package panels provides reusable Grafana panel builder functions for the
// ZFS dashboard generator. Each function returns a cog.Builder[dashboard.Panel]
// that can be added to any dashboard.
package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
)

// Namespace is the Prometheus metric prefix used in all PromQL expressions.
const Namespace = "zfs"

// DSRef returns a DataSourceRef pointing at the $datasource template variable.
func DSRef() common.DataSourceRef {
	return common.DataSourceRef{
		Type: cog.ToPtr("prometheus"),
		Uid:  cog.ToPtr("${datasource}"),
	}
}

// PromQuery creates a Prometheus range query builder with common defaults.
func PromQuery(expr, legendFormat, refID string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expr).
		LegendFormat(legendFormat).
		RefId(refID)
}

// PromInstantQuery creates a Prometheus instant query builder for table panels.
func PromInstantQuery(expr, legendFormat, refID string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expr).
		LegendFormat(legendFormat).
		RefId(refID).
		Instant().
		Format(prometheus.PromQueryFormatTable)
}

// ThresholdsGreenOnly returns a threshold config with a single green step
// (base color, no value trigger). Used for panels without conditional coloring.
func ThresholdsGreenOnly() *dashboard.ThresholdsConfigBuilder {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "green"},
		})
}

// ThresholdsRedGreen returns a threshold config that shows red below the
// threshold value and green at or above it.
func ThresholdsRedGreen(greenAbove float64) *dashboard.ThresholdsConfigBuilder {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "red"},
			{Value: cog.ToPtr(greenAbove), Color: "green"},
		})
}

// ThresholdsGreenYellowRed returns a threshold config with green (base),
// yellow at a warning level, and red at a critical level.
func ThresholdsGreenYellowRed(yellow, red float64) *dashboard.ThresholdsConfigBuilder {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "green"},
			{Value: cog.ToPtr(yellow), Color: "yellow"},
			{Value: cog.ToPtr(red), Color: "red"},
		})
}

// ThresholdsGreenYellowRedPercent returns a percentage-mode threshold config.
func ThresholdsGreenYellowRedPercent(yellow, red float64) *dashboard.ThresholdsConfigBuilder {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModePercentage).
		Steps([]dashboard.Threshold{
			{Value: nil, Color: "green"},
			{Value: cog.ToPtr(yellow), Color: "yellow"},
			{Value: cog.ToPtr(red), Color: "red"},
		})
}

// ColorSchemeThresholds returns a FieldColor configured for threshold-based
// coloring. Used by stat panels and bar gauges.
func ColorSchemeThresholds() *dashboard.FieldColorBuilder {
	return dashboard.NewFieldColorBuilder().
		Mode(dashboard.FieldColorModeIdThresholds)
}

// ColorSchemePaletteClassic returns a FieldColor configured for the classic
// multi-color palette. Used by timeseries panels.
func ColorSchemePaletteClassic() *dashboard.FieldColorBuilder {
	return dashboard.NewFieldColorBuilder().
		Mode(dashboard.FieldColorModeIdPaletteClassic)
}

// PoolFilter returns the PromQL pool label filter for the $pool variable.
func PoolFilter() string {
	return `pool=~"$pool"`
}

// ServiceFilter returns a PromQL filter matching a specific service key.
func ServiceFilter(serviceKey string) string {
	return `service="` + serviceKey + `"`
}
