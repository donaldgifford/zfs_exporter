package panels

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/table"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// GrowthRate returns a timeseries panel showing dataset daily growth rate.
func GrowthRate() cog.Builder[dashboard.Panel] {
	return timeseries.NewPanelBuilder().
		Title("Dataset Daily Growth Rate").
		Description("Estimated daily growth rate per dataset, derived from the 1-hour derivative of used bytes.").
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`deriv(zfs_dataset_used_bytes{%s}[1h]) * 86400`, PoolFilter()),
			"{{dataset}}", "A",
		)).
		Unit("bytes").
		AxisLabel("bytes / day").
		AxisCenteredZero(true).
		LineInterpolation(common.LineInterpolationSmooth).
		FillOpacity(10).
		ShowPoints(common.VisibilityModeNever).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull", "mean")).
		Tooltip(MultiTooltip())
}

// DeviationTable returns a table panel showing datasets outside their 7-day
// baseline. Uses recording rules for average and standard deviation.
func DeviationTable() cog.Builder[dashboard.Panel] {
	pf := PoolFilter()

	return table.NewPanelBuilder().
		Title("Datasets Outside Normal Range (7d Baseline)").
		Description("Datasets whose current usage deviates from their 7-day average by more than 2 standard deviations. Uses recording rules zfs:dataset_used_bytes:avg7d and zfs:dataset_used_bytes:stddev7d.").
		Datasource(DSRef()).
		WithTarget(PromInstantQuery(fmt.Sprintf(`zfs_dataset_used_bytes{%s}`, pf), "", "Current")).
		WithTarget(PromInstantQuery(fmt.Sprintf(`zfs:dataset_used_bytes:avg7d{%s}`, pf), "", "Avg7d")).
		WithTarget(PromInstantQuery(fmt.Sprintf(`zfs:dataset_used_bytes:stddev7d{%s}`, pf), "", "Stddev7d")).
		Thresholds(ThresholdsGreenYellowRed(2, 3)).
		ColorScheme(ColorSchemeThresholds()).
		OverrideByName("Current", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
		}).
		OverrideByName("7d Avg", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
		}).
		OverrideByName("Deviation", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
		}).
		OverrideByName("Sigma", []dashboard.DynamicConfigValue{
			{Id: "decimals", Value: 2},
			{Id: "custom.cellOptions", Value: map[string]any{"mode": "gradient", "type": "gauge"}},
		}).
		OverrideByName("dataset", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 250},
		}).
		CellHeight(common.TableCellHeightSm).
		ShowHeader(true).
		// Transformations: merge multi-query results, organize columns, calculate
		// deviation and sigma fields, sort by sigma descending.
		WithTransformation(dashboard.DataTransformerConfig{Id: "merge", Options: map[string]any{}}).
		WithTransformation(deviationOrganizeTransform()).
		WithTransformation(deviationCalcField("Deviation", "Current", "-", "7d Avg")).
		WithTransformation(deviationCalcField("Sigma", "Deviation", "/", "Std Dev")).
		WithTransformation(dashboard.DataTransformerConfig{
			Id: "sortBy",
			Options: map[string]any{
				"fields": map[string]any{},
				"sort": []any{
					map[string]any{"desc": true, "field": "Sigma"},
				},
			},
		})
}

func deviationOrganizeTransform() dashboard.DataTransformerConfig {
	exclude := map[string]any{}
	for _, prefix := range []string{"Time", "__name__", "instance", "job", "type", "pool", "dataset"} {
		for _, suffix := range []string{"", " 1", " 2", " 3"} {
			exclude[prefix+suffix] = true
		}
	}
	// Keep the base "pool" and "dataset" columns (remove numbered duplicates).
	delete(exclude, "pool")
	delete(exclude, "dataset")

	return dashboard.DataTransformerConfig{
		Id: "organize",
		Options: map[string]any{
			"excludeByName": exclude,
			"renameByName": map[string]any{
				"Value #Current":  "Current",
				"Value #Avg7d":    "7d Avg",
				"Value #Stddev7d": "Std Dev",
			},
		},
	}
}

func deviationCalcField(alias, left, operator, right string) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id: "calculateField",
		Options: map[string]any{
			"alias": alias,
			"mode":  "binary",
			"binary": map[string]any{
				"left":     left,
				"operator": operator,
				"reducer":  "sum",
				"right":    right,
			},
			"reduce": map[string]any{"reducer": "sum"},
		},
	}
}

// PoolFillPrediction returns a timeseries panel showing predicted days until
// each pool fills, based on 7-day linear trend.
func PoolFillPrediction() cog.Builder[dashboard.Panel] {
	return timeseries.NewPanelBuilder().
		Title("Pool Days Until Full (7d Trend)").
		Description("Predicted days until pool is full based on linear extrapolation of free bytes over the past 7 days. Lower values indicate pools at risk of running out of space.").
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_pool_free_bytes{%s} / (-deriv(zfs_pool_free_bytes{%s}[7d])) / 86400 > 0`, PoolFilter(), PoolFilter()),
			"{{pool}}", "A",
		)).
		Unit("d").
		AxisLabel("days").
		Min(0).
		LineInterpolation(common.LineInterpolationSmooth).
		LineWidth(2).
		FillOpacity(10).
		ShowPoints(common.VisibilityModeNever).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "transparent"},
					{Value: cog.ToPtr(0.0), Color: "red"},
					{Value: cog.ToPtr(7.0), Color: "orange"},
					{Value: cog.ToPtr(30.0), Color: "yellow"},
					{Value: cog.ToPtr(90.0), Color: "transparent"},
				}),
		).
		ThresholdsStyle(
			common.NewGraphThresholdsStyleConfigBuilder().
				Mode(common.GraphThresholdsStyleModeLineAndArea),
		).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull", "min")).
		Tooltip(
			common.NewVizTooltipOptionsBuilder().
				Mode(common.TooltipDisplayModeMulti).
				Sort(common.SortOrderAscending),
		)
}
