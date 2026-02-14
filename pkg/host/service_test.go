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

// unitResponse describes what a mock runner returns for a given systemctl call.
type unitResponse struct {
	loadState string // value for "systemctl show --property=LoadState" (e.g. "loaded", "not-found")
	isActive  string // value for "systemctl is-active" (e.g. "active", "inactive", "failed")
	isActErr  error  // error returned by "systemctl is-active" (non-nil for inactive/failed)
}

// mockRunner creates a Runner that dispatches by unit name. It handles both
// "systemctl show --property=LoadState <unit>" and "systemctl is-active <unit>".
func mockRunner(responses map[string]unitResponse) zfs.Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" || len(args) == 0 {
			return nil, errors.New("unexpected command")
		}

		// "systemctl show --property=LoadState <unit>"
		if args[0] == "show" {
			unit := args[len(args)-1]
			if r, ok := responses[unit]; ok {
				return []byte("LoadState=" + r.loadState + "\n"), nil
			}

			return []byte("LoadState=not-found\n"), nil
		}

		// "systemctl is-active <unit>"
		if args[0] == "is-active" {
			unit := args[len(args)-1]
			if r, ok := responses[unit]; ok {
				return []byte(r.isActive + "\n"), r.isActErr
			}

			return []byte("inactive\n"), errors.New("exit status 3")
		}

		return nil, errors.New("unknown systemctl subcommand")
	}
}

func TestCheckServices_ActiveService(t *testing.T) {
	runner := mockRunner(map[string]unitResponse{
		"nfs-kernel-server.service": {loadState: "loaded", isActive: "active"},
	})

	checker := NewServiceChecker(runner, testLogger())
	statuses, err := checker.CheckServices(context.Background(), map[string][]string{
		"nfs": {"nfs-kernel-server.service"},
	})
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
	runner := mockRunner(map[string]unitResponse{
		"smbd.service": {loadState: "loaded", isActive: "inactive", isActErr: errors.New("exit status 3")},
	})

	checker := NewServiceChecker(runner, testLogger())
	statuses, err := checker.CheckServices(context.Background(), map[string][]string{
		"smb": {"smbd.service"},
	})
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
	runner := mockRunner(map[string]unitResponse{
		"zfs-zed.service": {loadState: "loaded", isActive: "failed", isActErr: errors.New("exit status 3")},
	})

	checker := NewServiceChecker(runner, testLogger())
	statuses, err := checker.CheckServices(context.Background(), map[string][]string{
		"zfs": {"zfs-zed.service"},
	})
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
	runner := mockRunner(map[string]unitResponse{
		// First unit doesn't exist, second is active.
		"nfs-kernel-server.service": {loadState: "not-found"},
		"nfs-server.service":        {loadState: "loaded", isActive: "active"},
	})

	checker := NewServiceChecker(runner, testLogger())
	statuses, err := checker.CheckServices(context.Background(), map[string][]string{
		"nfs": {"nfs-kernel-server.service", "nfs-server.service"},
	})
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
	runner := mockRunner(map[string]unitResponse{
		"tgt.service":         {loadState: "not-found"},
		"iscsitarget.service": {loadState: "not-found"},
	})

	checker := NewServiceChecker(runner, testLogger())
	statuses, err := checker.CheckServices(context.Background(), map[string][]string{
		"iscsi": {"tgt.service", "iscsitarget.service"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 0 {
		t.Fatalf("expected 0 statuses (all units missing), got %d", len(statuses))
	}
}

func TestCheckServices_MultipleServices(t *testing.T) {
	runner := mockRunner(map[string]unitResponse{
		"zfs-zed.service":           {loadState: "loaded", isActive: "active"},
		"nfs-kernel-server.service": {loadState: "loaded", isActive: "inactive", isActErr: errors.New("exit 3")},
		"smbd.service":              {loadState: "loaded", isActive: "active"},
		"tgt.service":               {loadState: "not-found"},
		"iscsitarget.service":       {loadState: "not-found"},
	})

	checker := NewServiceChecker(runner, testLogger())
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
	var calls []string

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))

		if args[0] == "show" {
			return []byte("LoadState=loaded\n"), nil
		}

		return []byte("active\n"), nil
	}

	checker := NewServiceChecker(runner, testLogger())

	_, _ = checker.CheckServices(context.Background(), map[string][]string{
		"test": {"test.service"},
	})

	if len(calls) != 2 {
		t.Fatalf("expected 2 systemctl calls, got %d: %v", len(calls), calls)
	}

	expectedShow := "systemctl show --property=LoadState test.service"
	if calls[0] != expectedShow {
		t.Errorf("first call = %q, want %q", calls[0], expectedShow)
	}

	expectedIsActive := "systemctl is-active test.service"
	if calls[1] != expectedIsActive {
		t.Errorf("second call = %q, want %q", calls[1], expectedIsActive)
	}
}
