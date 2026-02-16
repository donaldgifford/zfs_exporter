package panels

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/table"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// Default grid sizes for dataset panels.
const (
	datasetTableWidth    = 8
	datasetTableHeight   = 9
	datasetTSWidth       = 8
	datasetTSHeight      = 9
)

// organizeTransform returns a DataTransformerConfig that hides internal labels
// and reorders columns for table panels.
func organizeTransform(excludeByName map[string]bool, indexByName map[string]int) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id: "organize",
		Options: map[string]any{
			"excludeByName": excludeByName,
			"indexByName":   indexByName,
			"renameByName":  map[string]any{},
		},
	}
}

// datasetTableExcludes returns the standard set of internal labels to hide in
// dataset table panels.
func datasetTableExcludes() map[string]bool {
	return map[string]bool{
		"Time":     true,
		"__name__": true,
		"instance": true,
		"job":      true,
	}
}

// datasetTableColumnOrder returns the standard column ordering for dataset tables.
func datasetTableColumnOrder() map[string]int {
	return map[string]int{
		"dataset": 0,
		"pool":    1,
		"type":    2,
		"Value":   3,
	}
}

// TopDatasets returns a table panel showing top datasets ranked by used space.
func TopDatasets() *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("Top Datasets by Used Space").
		Description("Top datasets ranked by current used space, sorted descending.").
		Height(datasetTableHeight).
		Span(datasetTableWidth).
		Datasource(DSRef()).
		WithTarget(PromInstantQuery(
			fmt.Sprintf(`topk(25, zfs_dataset_used_bytes{%s})`, PoolFilter()),
			"", "A",
		)).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "green"},
					{Value: cog.ToPtr(107374182400.0), Color: "yellow"}, // 100 GiB
					{Value: cog.ToPtr(1099511627776.0), Color: "red"},   // 1 TiB
				}),
		).
		ColorScheme(ColorSchemeThresholds()).
		OverrideByName("Value", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
			{Id: "displayName", Value: "Used"},
			{Id: "custom.cellOptions", Value: map[string]any{"mode": "gradient", "type": "gauge"}},
		}).
		OverrideByName("dataset", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 280},
		}).
		CellHeight(common.TableCellHeightSm).
		ShowHeader(true).
		WithTransformation(organizeTransform(datasetTableExcludes(), datasetTableColumnOrder()))
}

// AvailableSpace returns a table panel showing available space per dataset.
func AvailableSpace() *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("Dataset Available Space").
		Description("Available space per dataset with pool and type information.").
		Height(datasetTableHeight).
		Span(datasetTableWidth).
		Datasource(DSRef()).
		WithTarget(PromInstantQuery(
			fmt.Sprintf(`zfs_dataset_available_bytes{%s}`, PoolFilter()),
			"", "A",
		)).
		Thresholds(
			dashboard.NewThresholdsConfigBuilder().
				Mode(dashboard.ThresholdsModeAbsolute).
				Steps([]dashboard.Threshold{
					{Value: nil, Color: "red"},
					{Value: cog.ToPtr(10737418240.0), Color: "yellow"}, // 10 GiB
					{Value: cog.ToPtr(107374182400.0), Color: "green"}, // 100 GiB
				}),
		).
		ColorScheme(ColorSchemeThresholds()).
		OverrideByName("Value", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
			{Id: "displayName", Value: "Available"},
			{Id: "custom.cellOptions", Value: map[string]any{"mode": "gradient", "type": "gauge"}},
		}).
		OverrideByName("dataset", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 280},
		}).
		CellHeight(common.TableCellHeightSm).
		ShowHeader(true).
		WithTransformation(organizeTransform(datasetTableExcludes(), datasetTableColumnOrder()))
}

// DatasetUsageOverTime returns a timeseries panel showing used bytes per dataset.
func DatasetUsageOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Dataset Usage Over Time").
		Description("Dataset used bytes over time, per dataset.").
		Height(datasetTSHeight).
		Span(datasetTSWidth).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_dataset_used_bytes{%s}`, PoolFilter()),
			"{{dataset}}", "A",
		)).
		Unit("bytes").
		FillOpacity(5).
		ShowPoints(common.VisibilityModeNever).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull")).
		Tooltip(MultiTooltip())
}
