package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"gopkg.in/yaml.v3"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/dashboards"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/rules"
)

// TestStaleness verifies that committed dashboard JSON and rules YAML files
// match what the generator produces. If this test fails, run `make dashboards`
// to regenerate.
func TestStaleness(t *testing.T) {
	cfg := DefaultConfig
	svcs := toServiceConfigs(cfg.Services)

	t.Run("zfs-status.json", func(t *testing.T) {
		b, err := dashboards.BuildStatus(dashboards.StatusConfig{Services: svcs})
		if err != nil {
			t.Fatal(err)
		}
		assertDashboardFresh(t, cfg.OutputDir, "zfs-status.json", b)
	})

	t.Run("zfs-details.json", func(t *testing.T) {
		b, err := dashboards.BuildDetails(dashboards.DetailsConfig{Services: svcs})
		if err != nil {
			t.Fatal(err)
		}
		assertDashboardFresh(t, cfg.OutputDir, "zfs-details.json", b)
	})

	t.Run("zfs-combined.json", func(t *testing.T) {
		b, err := dashboards.BuildCombined(dashboards.CombinedConfig{Services: svcs})
		if err != nil {
			t.Fatal(err)
		}
		assertDashboardFresh(t, cfg.OutputDir, "zfs-combined.json", b)
	})

	t.Run("zfs-recording-rules.yaml", func(t *testing.T) {
		assertRulesFresh(t, cfg.RulesDir(), "zfs-recording-rules.yaml", rules.RecordingPrometheusRule())
	})

	t.Run("zfs-alerts.yaml", func(t *testing.T) {
		rsvcs := toRulesServiceConfigs(cfg.Services)
		assertRulesFresh(t, cfg.RulesDir(), "zfs-alerts.yaml", rules.AlertPrometheusRule(rsvcs))
	})
}

func assertDashboardFresh(t *testing.T, dir, filename string, b *dashboard.DashboardBuilder) {
	t.Helper()
	dash, err := b.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	want, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want = append(want, '\n')

	path := filepath.Join(dir, filename)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading committed file %s: %v", path, err)
	}

	if string(got) != string(want) {
		t.Errorf("%s is stale — run `make dashboards` to regenerate", filename)
	}
}

func assertRulesFresh(t *testing.T, dir, filename string, rf any) {
	t.Helper()

	body, err := yaml.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := append([]byte("---\n"), body...)

	path := filepath.Join(dir, filename)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading committed file %s: %v", path, err)
	}

	if string(got) != string(want) {
		t.Errorf("%s is stale — run `make dashboards` to regenerate", filename)
	}
}
