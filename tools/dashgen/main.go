// dashgen generates Grafana dashboard JSON files and Prometheus rules YAML from
// a Go config struct using the Grafana Foundation SDK. Run via go generate.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"gopkg.in/yaml.v3"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/dashboards"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/rules"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/validate"
)

func main() {
	validateOnly := flag.Bool("validate", false, "validate dashboards without writing files")
	flag.Parse()

	cfg := DefaultConfig

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed:\n%v", err)
	}

	type dashEntry struct {
		filename string
		builder  func(cfg Config) (*dashboard.DashboardBuilder, error)
	}

	entries := []dashEntry{}

	if cfg.Dashboards.Status {
		entries = append(entries, dashEntry{"zfs-status.json", buildStatusDashboard})
	}

	if cfg.Dashboards.Details {
		entries = append(entries, dashEntry{"zfs-details.json", buildDetailsDashboard})
	}

	if cfg.Dashboards.Combined {
		entries = append(entries, dashEntry{"zfs-combined.json", buildCombinedDashboard})
	}

	hasErrors := false

	for _, e := range entries {
		builder, err := e.builder(cfg)
		if err != nil {
			log.Fatalf("building %s: %v", e.filename, err)
		}

		dash, err := builder.Build()
		if err != nil {
			log.Fatalf("finalizing %s: %v", e.filename, err)
		}

		// Run validation on every dashboard.
		result := validate.Dashboard(dash)
		output := validate.FormatResult(e.filename, result)
		if output != "" {
			fmt.Print(output)
		}
		if !result.Ok() {
			hasErrors = true
		}

		if *validateOnly {
			continue
		}

		if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
			log.Fatalf("creating output directory: %v", err)
		}

		data, err := json.MarshalIndent(dash, "", "  ")
		if err != nil {
			log.Fatalf("marshaling %s: %v", e.filename, err)
		}

		// Append trailing newline for POSIX compliance.
		data = append(data, '\n')

		path := filepath.Join(cfg.OutputDir, e.filename)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatalf("writing %s: %v", path, err)
		}

		fmt.Printf("wrote %s\n", path)
	}

	// Generate Prometheus rules (skip in validate-only mode).
	if !*validateOnly {
		generateRules(cfg)
	}

	if hasErrors {
		os.Exit(1)
	}
}

func generateRules(cfg Config) {
	rulesDir := filepath.Join(cfg.RulesDir())

	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		log.Fatalf("creating rules directory: %v", err)
	}

	svcConfigs := toRulesServiceConfigs(cfg.Services)

	// Recording rules.
	writeYAML(rulesDir, "recording_rules.yml", rules.RecordingRules())

	// Alert rules.
	writeYAML(rulesDir, "alerts.yml", rules.AlertRules(svcConfigs))
}

func writeYAML(dir, filename string, v any) {
	data, err := yaml.Marshal(v)
	if err != nil {
		log.Fatalf("marshaling %s: %v", filename, err)
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("writing %s: %v", path, err)
	}

	fmt.Printf("wrote %s\n", path)
}

// toServiceConfigs converts the main config's ServiceConfig slice to the
// panels package's ServiceConfig type.
func toServiceConfigs(svcs []ServiceConfig) []panels.ServiceConfig {
	out := make([]panels.ServiceConfig, len(svcs))
	for i, s := range svcs {
		out[i] = panels.ServiceConfig{
			Key:         s.Key,
			Label:       s.Label,
			ShareMetric: s.ShareMetric,
			UseZvols:    s.UseZvols,
		}
	}
	return out
}

// toRulesServiceConfigs converts the main config's ServiceConfig slice to the
// rules package's ServiceConfig type.
func toRulesServiceConfigs(svcs []ServiceConfig) []rules.ServiceConfig {
	out := make([]rules.ServiceConfig, len(svcs))
	for i, s := range svcs {
		out[i] = rules.ServiceConfig{
			Key:         s.Key,
			Label:       s.Label,
			ShareMetric: s.ShareMetric,
		}
	}
	return out
}

func buildStatusDashboard(cfg Config) (*dashboard.DashboardBuilder, error) {
	return dashboards.BuildStatus(dashboards.StatusConfig{
		Services: toServiceConfigs(cfg.Services),
	})
}

func buildDetailsDashboard(cfg Config) (*dashboard.DashboardBuilder, error) {
	return dashboards.BuildDetails(dashboards.DetailsConfig{
		Services: toServiceConfigs(cfg.Services),
	})
}

func buildCombinedDashboard(cfg Config) (*dashboard.DashboardBuilder, error) {
	return dashboards.BuildCombined(dashboards.CombinedConfig{
		Services: toServiceConfigs(cfg.Services),
	})
}
