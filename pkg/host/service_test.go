package host

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/donaldgifford/zfs_exporter/pkg/zfs"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, nil))
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestCheckServices_ActiveService(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("active\n"), nil
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"nfs": {"nfs-kernel-server.service"},
	}

	statuses, err := checker.CheckServices(context.Background(), services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if !statuses[0].Active {
		t.Error("expected service to be active")
	}

	if statuses[0].Name != "nfs" {
		t.Errorf("expected service name %q, got %q", "nfs", statuses[0].Name)
	}
}

func TestCheckServices_InactiveService(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("inactive\n"), errors.New("exit status 3")
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"smb": {"smbd.service"},
	}

	statuses, err := checker.CheckServices(context.Background(), services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if statuses[0].Active {
		t.Error("expected service to be inactive")
	}
}

func TestCheckServices_FailedService(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("failed\n"), errors.New("exit status 3")
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"zfs": {"zfs-zed.service"},
	}

	statuses, err := checker.CheckServices(context.Background(), services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if statuses[0].Active {
		t.Error("expected service to not be active")
	}
}

func TestCheckServices_UnitNotFound_TriesFallback(t *testing.T) {
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// Simulate: first unit doesn't exist (no output), second is active.
		unit := args[len(args)-1]
		if unit == "nfs-kernel-server.service" {
			return []byte(""), errors.New("unit not found")
		}

		return []byte("active\n"), nil
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"nfs": {"nfs-kernel-server.service", "nfs-server.service"},
	}

	statuses, err := checker.CheckServices(context.Background(), services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if !statuses[0].Active {
		t.Error("expected service to be active via fallback unit")
	}
}

func TestCheckServices_NoUnitsExist(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(""), errors.New("unit not found")
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"iscsi": {"tgt.service", "iscsitarget.service"},
	}

	statuses, err := checker.CheckServices(context.Background(), services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 0 {
		t.Fatalf("expected 0 statuses (all units missing), got %d", len(statuses))
	}
}

func TestCheckServices_MultipleServices(t *testing.T) {
	responses := map[string]struct {
		output string
		err    error
	}{
		"zfs-zed.service":           {"active\n", nil},
		"nfs-kernel-server.service": {"inactive\n", errors.New("exit 3")},
		"smbd.service":              {"active\n", nil},
		"tgt.service":               {"", errors.New("unit not found")},
		"iscsitarget.service":       {"", errors.New("unit not found")},
	}

	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		unit := args[len(args)-1]
		if r, ok := responses[unit]; ok {
			return []byte(r.output), r.err
		}

		return []byte(""), errors.New("unknown unit")
	}

	checker := NewServiceChecker(zfs.Runner(runner), testLogger())

	statuses, err := checker.CheckServices(context.Background(), DefaultServiceUnits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// iscsi should be skipped (no units exist), so we expect 3.
	if len(statuses) != 3 {
		names := make([]string, 0, len(statuses))
		for _, s := range statuses {
			names = append(names, s.Name)
		}
		t.Fatalf("expected 3 statuses, got %d: %v", len(statuses), names)
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	// Verify each service status.
	for _, tc := range []struct {
		name   string
		active bool
	}{
		{"nfs", false},
		{"smb", true},
		{"zfs", true},
	} {
		found := false
		for _, s := range statuses {
			if s.Name == tc.name {
				found = true
				if s.Active != tc.active {
					t.Errorf("service %q active = %v, want %v", tc.name, s.Active, tc.active)
				}
			}
		}
		if !found {
			t.Errorf("service %q not found in results", tc.name)
		}
	}
}

func TestCheckServices_RunnerUsesSystemctl(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		capturedName = name
		capturedArgs = args

		return []byte("active\n"), nil
	}

	checker := NewServiceChecker(runner, testLogger())

	services := map[string][]string{
		"test": {"test.service"},
	}

	_, _ = checker.CheckServices(context.Background(), services)

	if capturedName != "systemctl" {
		t.Errorf("expected command %q, got %q", "systemctl", capturedName)
	}

	expectedArgs := "is-active test.service"
	gotArgs := strings.Join(capturedArgs, " ")

	if gotArgs != expectedArgs {
		t.Errorf("expected args %q, got %q", expectedArgs, gotArgs)
	}
}
