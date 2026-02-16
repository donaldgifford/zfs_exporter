package panels

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/bargauge"
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// Default grid sizes for pool panels.
const (
	poolStatWidth      = 6
	poolStatHeight     = 4
	poolTSWidth        = 12
	poolTSHeight       = 8
	poolBarGaugeWidth  = 6
	poolBarGaugeHeight = 8
	poolFragWidth      = 8
	poolFragHeight     = 8
)

// PoolHealth returns a stat panel showing whether pools are ONLINE.
func PoolHealth() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Pool Health").
		Description("Pool online/offline status. Shows ONLINE when the health metric equals 1.").
		Height(poolStatHeight).
		Span(poolStatWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_health{state="online", %s}`, PoolFilter()),
			"{{ pool }}", "A",
		)).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("NOT ONLINE", "red", "ONLINE", "green"),
		})
}

// PoolCapacity returns a stat panel showing pool capacity as a percentage.
func PoolCapacity() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Pool Capacity").
		Description("Allocated bytes as a fraction of total pool size.").
		Height(poolStatHeight).
		Span(poolStatWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_allocated_bytes{%s} / zfs_pool_size_bytes{%s}`, PoolFilter(), PoolFilter()),
			"{{ pool }}", "A",
		)).
		Unit("percentunit").
		Decimals(1).
		Min(0).
		Max(1).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsGreenYellowRed(0.8, 0.9)).
		ColorScheme(ColorSchemeThresholds())
}

// ResilverScrub returns a stat panel showing resilver/scrub activity.
func ResilverScrub() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Resilver/Scrub Status").
		Description("Active resilver or scrub operations. IDLE when no operations are running.").
		Height(poolStatHeight).
		Span(poolStatWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_resilver_active{%s}`, PoolFilter()),
			"{{ pool }} resilver", "A",
		)).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_scrub_active{%s}`, PoolFilter()),
			"{{ pool }} scrub", "B",
		)).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "green"},
					{Value: cog.ToPtr(1.0), Color: "orange"},
				}),
		).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("IDLE", "green", "ACTIVE", "orange"),
		})
}

// DaysUntilFull returns a stat panel showing estimated days until pool fills.
func DaysUntilFull() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Pool Days Until Full").
		Description("Estimated days until pool reaches full capacity based on 7-day linear trend. Negative values (pool shrinking) display as 'Not filling'. Higher is better.").
		Height(poolStatHeight).
		Span(poolStatWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_free_bytes{%s} / (-deriv(zfs_pool_free_bytes{%s}[7d])) / 86400`, PoolFilter(), PoolFilter()),
			"{{ pool }}", "A",
		)).
		Unit("d").
		Decimals(0).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "red"},
					{Value: cog.ToPtr(7.0), Color: "yellow"},
					{Value: cog.ToPtr(30.0), Color: "green"},
				}),
		).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			RangeMapping(cog.ToPtr(-1e15), cog.ToPtr(0.0), "Not filling", "green", 0),
		})
}

// PoolUsageOverTime returns a timeseries panel showing allocated and free bytes.
func PoolUsageOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Pool Usage Over Time").
		Description("Pool allocated and free bytes over time.").
		Height(poolTSHeight).
		Span(poolTSWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_allocated_bytes{%s}`, PoolFilter()),
			"{{pool}} allocated", "A",
		)).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_free_bytes{%s}`, PoolFilter()),
			"{{pool}} free", "B",
		)).
		Unit("bytes").
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull")).
		Tooltip(MultiTooltip()).
		Stacking(
			common.NewStackingConfigBuilder().
				Mode(common.StackingModeNormal),
		)
}

// PoolUsageBars returns a bar gauge showing pool usage percentage per pool.
func PoolUsageBars() *bargauge.PanelBuilder {
	return bargauge.NewPanelBuilder().
		Title("Pool Usage % (Allocated / Total)").
		Description("Current allocated bytes compared to total pool size.").
		Height(poolBarGaugeHeight).
		Span(poolBarGaugeWidth).
		Datasource(DSRef()).
		WithTarget(
			PromInstantQuery(
				fmt.Sprintf(`zfs_pool_allocated_bytes{%s} / zfs_pool_size_bytes{%s}`, PoolFilter(), PoolFilter()),
				"{{pool}}", "A",
			),
		).
		Unit("percentunit").
		Min(0).
		Max(1).
		Orientation(common.VizOrientationHorizontal).
		DisplayMode(common.BarGaugeDisplayModeGradient).
		ValueMode(common.BarGaugeValueModeColor).
		ShowUnfilled(true).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "green"},
					{Value: cog.ToPtr(0.7), Color: "yellow"},
					{Value: cog.ToPtr(0.8), Color: "orange"},
					{Value: cog.ToPtr(0.9), Color: "red"},
				}),
		).
		ColorScheme(
			dashboard.NewFieldColorBuilder().
				Mode(dashboard.FieldColorModeIdContinuousBlYlRd),
		)
}

// Fragmentation returns a timeseries panel showing pool fragmentation over time.
func Fragmentation() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Fragmentation Over Time").
		Description("Pool fragmentation ratio over time. High fragmentation can degrade performance.").
		Height(poolFragHeight).
		Span(poolFragWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_fragmentation_ratio{%s}`, PoolFilter()),
			"{{pool}}", "A",
		)).
		Unit("percentunit").
		LineInterpolation(common.LineInterpolationSmooth).
		LineWidth(2).
		FillOpacity(10).
		ShowPoints(common.VisibilityModeNever).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "transparent"},
					{Value: cog.ToPtr(0.5), Color: "red"},
				}),
		).
		ThresholdsStyle(
			common.NewGraphThresholdsStyleConfigBuilder().
				Mode(common.GraphThresholdsStyleModeLineAndArea),
		).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull", "max")).
		Tooltip(MultiTooltip())
}
