// dashgen generates Grafana dashboard JSON files from a Go config struct
// using the Grafana Foundation SDK. Run via go generate.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/dashboards"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
)

func main() {
	cfg := DefaultConfig

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed:\n%v", err)
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		log.Fatalf("creating output directory: %v", err)
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

	for _, e := range entries {
		builder, err := e.builder(cfg)
		if err != nil {
			log.Fatalf("building %s: %v", e.filename, err)
		}

		dash, err := builder.Build()
		if err != nil {
			log.Fatalf("finalizing %s: %v", e.filename, err)
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

func buildStatusDashboard(cfg Config) (*dashboard.DashboardBuilder, error) {
	return dashboards.BuildStatus(dashboards.StatusConfig{
		Services: toServiceConfigs(cfg.Services),
	})
}

func buildDetailsDashboard(_ Config) (*dashboard.DashboardBuilder, error) {
	return nil, fmt.Errorf("not yet implemented")
}

func buildCombinedDashboard(_ Config) (*dashboard.DashboardBuilder, error) {
	return nil, fmt.Errorf("not yet implemented")
}
