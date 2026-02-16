package main

import (
	"encoding/json"
	"testing"

	"github.com/donaldgifford/zfs_exporter/tools/dashgen/dashboards"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/panels"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/rules"
	"github.com/donaldgifford/zfs_exporter/tools/dashgen/validate"
)

var testServices = []panels.ServiceConfig{
	{Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
	{Key: "smb", Label: "SMB", ShareMetric: "zfs_dataset_share_smb"},
	{Key: "iscsi", Label: "iSCSI", UseZvols: true},
}

func TestDefaultConfigValid(t *testing.T) {
	cfg := DefaultConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig validation failed: %v", err)
	}
}

func TestBuildStatusDashboard(t *testing.T) {
	b, err := dashboards.BuildStatus(dashboards.StatusConfig{Services: testServices})
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}

	dash, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result := validate.Dashboard(dash)
	if !result.Ok() {
		t.Errorf("validation errors: %v", result.Errors)
	}

	data, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if len(data) < 100 {
		t.Error("generated JSON suspiciously small")
	}

	assertJSONField(t, data, "uid", "zfs-status")
	assertJSONField(t, data, "title", "ZFS Status")
}

func TestBuildDetailsDashboard(t *testing.T) {
	b, err := dashboards.BuildDetails(dashboards.DetailsConfig{Services: testServices})
	if err != nil {
		t.Fatalf("BuildDetails: %v", err)
	}

	dash, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result := validate.Dashboard(dash)
	if !result.Ok() {
		t.Errorf("validation errors: %v", result.Errors)
	}

	data, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertJSONField(t, data, "uid", "zfs-details")
	assertJSONField(t, data, "title", "ZFS Details")
}

func TestBuildCombinedDashboard(t *testing.T) {
	b, err := dashboards.BuildCombined(dashboards.CombinedConfig{Services: testServices})
	if err != nil {
		t.Fatalf("BuildCombined: %v", err)
	}

	dash, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result := validate.Dashboard(dash)
	if !result.Ok() {
		t.Errorf("validation errors: %v", result.Errors)
	}

	data, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertJSONField(t, data, "uid", "zfs-combined")
	assertJSONField(t, data, "title", "ZFS Combined")
}

func TestRecordingRules(t *testing.T) {
	rf := rules.RecordingRules()
	if len(rf.Groups) == 0 {
		t.Fatal("expected at least one rule group")
	}
	g := rf.Groups[0]
	if g.Name != "zfs_anomaly_baselines" {
		t.Errorf("group name = %q, want %q", g.Name, "zfs_anomaly_baselines")
	}
	if len(g.Rules) < 5 {
		t.Errorf("expected at least 5 recording rules, got %d", len(g.Rules))
	}
}

func TestAlertRules(t *testing.T) {
	svcs := []rules.ServiceConfig{
		{Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
		{Key: "smb", Label: "SMB", ShareMetric: "zfs_dataset_share_smb"},
	}

	rf := rules.AlertRules(svcs)
	if len(rf.Groups) == 0 {
		t.Fatal("expected at least one rule group")
	}

	ruleNames := make(map[string]bool)
	for _, r := range rf.Groups[0].Rules {
		if r.Alert != "" {
			ruleNames[r.Alert] = true
		}
	}

	// Verify key alerts exist.
	for _, want := range []string{
		"ZfsExporterDown",
		"ZfsPoolDegraded",
		"ZfsPoolCapacityWarning",
		"ZfsServiceDown",
		"ZfsNFSSharesWithoutService",
		"ZfsSMBSharesWithoutService",
		"ZfsDatasetAbnormalGrowth",
		"ZfsPoolPredictedFull7d",
	} {
		if !ruleNames[want] {
			t.Errorf("missing expected alert %q", want)
		}
	}
}

func TestAlertRulesNoShareServices(t *testing.T) {
	// With no share-metric services, mismatch alerts should be absent.
	svcs := []rules.ServiceConfig{
		{Key: "iscsi", Label: "iSCSI"},
	}

	rf := rules.AlertRules(svcs)
	for _, r := range rf.Groups[0].Rules {
		if r.Alert == "ZfsISCSISharesWithoutService" {
			t.Error("unexpected mismatch alert for iSCSI (no ShareMetric)")
		}
	}
}

func assertJSONField(t *testing.T, data []byte, key, want string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, ok := m[key]
	if !ok {
		t.Errorf("missing field %q", key)
		return
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("unmarshal field %q: %v", key, err)
		return
	}
	if got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}
