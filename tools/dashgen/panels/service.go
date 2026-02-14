package panels

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/table"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// ServiceConfig mirrors the main config's ServiceConfig. The panels package
// uses this to avoid importing the main package.
type ServiceConfig struct {
	Key         string
	Label       string
	ShareMetric string
	UseZvols    bool
}

// ServiceStatusAll returns a stat panel showing all monitored service statuses.
func ServiceStatusAll() cog.Builder[dashboard.Panel] {
	return stat.NewPanelBuilder().
		Title("Service Status").
		Description("Shows whether monitored systemd services (ZFS, NFS, SMB, iSCSI) are active. Green = running, Red = down.").
		Datasource(DSRef()).
		WithTarget(PromQuery(
			"zfs_service_up",
			"{{ service }}", "A",
		)).
		Unit("none").
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("DOWN", "red", "UP", "green"),
		})
}

// ShareMismatch returns a stat panel detecting when shares exist but the
// service is down. Only applicable for services with a ShareMetric.
func ShareMismatch(svc ServiceConfig) cog.Builder[dashboard.Panel] {
	expr := fmt.Sprintf(
		`(count(%s == 1) > 0) and (zfs_service_up{%s} == 0)`,
		svc.ShareMetric, ServiceFilter(svc.Key),
	)

	return stat.NewPanelBuilder().
		Title(fmt.Sprintf("%s Share Mismatch", svc.Label)).
		Description(fmt.Sprintf("%s shares exist but %s service is down", svc.Label, svc.Label)).
		Datasource(DSRef()).
		WithTarget(PromQuery(expr, "", "A")).
		Unit("none").
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsGreenYellowRed(1, 1)).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			{
				SpecialValueMap: &dashboard.SpecialValueMap{
					Type: dashboard.MappingTypeSpecialValue,
					Options: dashboard.DashboardSpecialValueMapOptions{
						Match:  dashboard.SpecialValueMatchNullAndNan,
						Result: dashboard.ValueMappingResult{Text: cog.ToPtr("OK"), Color: cog.ToPtr("green"), Index: cog.ToPtr[int32](0)},
					},
				},
			},
			{
				ValueMap: &dashboard.ValueMap{
					Type: dashboard.MappingTypeValueToText,
					Options: map[string]dashboard.ValueMappingResult{
						"1": {Text: cog.ToPtr("MISMATCH"), Color: cog.ToPtr("red"), Index: cog.ToPtr[int32](1)},
					},
				},
			},
		})
}

// ExporterUp returns a stat panel showing whether the ZFS exporter is operational.
func ExporterUp() cog.Builder[dashboard.Panel] {
	return stat.NewPanelBuilder().
		Title("Exporter Up").
		Description("Shows whether the ZFS exporter itself is up and able to execute ZFS commands. Green = operational, Red = ZFS commands failing.").
		Datasource(DSRef()).
		WithTarget(PromQuery("zfs_up", "ZFS commands", "A")).
		Unit("none").
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("DOWN", "red", "UP", "green"),
		})
}

// ServiceStat returns a stat panel for a single service's up/down status.
func ServiceStat(svc ServiceConfig) cog.Builder[dashboard.Panel] {
	return stat.NewPanelBuilder().
		Title(fmt.Sprintf("%s Service", svc.Label)).
		Description(fmt.Sprintf("%s service up/down status.", svc.Label)).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_service_up{%s}`, ServiceFilter(svc.Key)),
			svc.Label, "A",
		)).
		Unit("none").
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("DOWN", "red", "UP", "green"),
		})
}

// ShareTable returns a table panel showing share datasets for a service.
// For services with UseZvols, it shows zvol inventory instead.
func ShareTable(svc ServiceConfig) cog.Builder[dashboard.Panel] {
	var expr, title string
	if svc.UseZvols {
		expr = fmt.Sprintf(`zfs_dataset_used_bytes{type="volume", %s}`, PoolFilter())
		title = fmt.Sprintf("%s Volumes (zvols)", svc.Label)
	} else {
		expr = fmt.Sprintf(`%s{%s} == 1`, svc.ShareMetric, PoolFilter())
		title = fmt.Sprintf("%s Shared Datasets", svc.Label)
	}

	overrides := []dashboard.DynamicConfigValue{
		{Id: "custom.hidden", Value: true},
	}

	b := table.NewPanelBuilder().
		Title(title).
		Description(fmt.Sprintf("Datasets shared via %s.", svc.Label)).
		Datasource(DSRef()).
		WithTarget(PromInstantQuery(expr, "", "A")).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemeThresholds()).
		OverrideByName("Time", overrides).
		CellHeight(common.TableCellHeightSm).
		ShowHeader(true)

	if svc.UseZvols {
		b = b.OverrideByName("Value", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "bytes"},
		})
		b = b.WithTransformation(dashboard.DataTransformerConfig{
			Id: "organize",
			Options: map[string]any{
				"excludeByName": map[string]any{
					"Time":     true,
					"__name__": true,
					"instance": true,
					"job":      true,
					"type":     true,
				},
				"indexByName":  map[string]any{},
				"renameByName": map[string]any{"Value": "Used", "dataset": "Dataset", "pool": "Pool"},
			},
		})
	} else {
		b = b.WithTransformation(dashboard.DataTransformerConfig{
			Id: "organize",
			Options: map[string]any{
				"excludeByName": map[string]any{
					"Time":     true,
					"__name__": true,
					"Value":    true,
					"instance": true,
					"job":      true,
				},
				"indexByName":  map[string]any{},
				"renameByName": map[string]any{"dataset": "Dataset", "pool": "Pool", "type": "Type"},
			},
		})
	}

	return b
}

// ServiceTimeline returns a timeseries panel showing service up/down over time.
func ServiceTimeline(svc ServiceConfig) cog.Builder[dashboard.Panel] {
	return timeseries.NewPanelBuilder().
		Title(fmt.Sprintf("%s Service Timeline", svc.Label)).
		Description(fmt.Sprintf("%s service up/down status over time. 1 = running, 0 = down.", svc.Label)).
		Datasource(DSRef()).
		WithTarget(PromQuery(
			fmt.Sprintf(`zfs_service_up{%s}`, ServiceFilter(svc.Key)),
			svc.Label, "A",
		)).
		Min(-0.2).
		Max(1.2).
		LineInterpolation(common.LineInterpolationStepAfter).
		LineWidth(2).
		FillOpacity(30).
		ShowPoints(common.VisibilityModeNever).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemePaletteClassic()).
		Legend(TableLegend("lastNotNull")).
		Tooltip(MultiTooltip()).
		Mappings([]dashboard.ValueMapping{
			ValueMapOnOff("Down", "red", "Up", "green"),
		})
}
