// Package host checks host-level service states via systemctl.
package host

import (
	"context"
	"log/slog"
	"strings"

	"github.com/donaldgifford/zfs_exporter/pkg/zfs"
)

// ServiceStatus represents the health of a systemd service.
type ServiceStatus struct {
	Name   string // service key (e.g. "nfs")
	Active bool   // true if systemd unit reports "active"
}

// DefaultServiceUnits maps service keys to candidate systemd unit names.
// The exporter tries each unit in order until one exists.
var DefaultServiceUnits = map[string][]string{
	"zfs":   {"zfs-zed.service"},
	"nfs":   {"nfs-kernel-server.service", "nfs-server.service"},
	"smb":   {"smbd.service", "smb.service"},
	"iscsi": {"iscsid.socket", "iscsid.service", "iscsi.service", "tgt.service", "iscsitarget.service"},
}

// ServiceChecker checks systemd service states.
type ServiceChecker struct {
	runner zfs.Runner
	logger *slog.Logger
}

// NewServiceChecker creates a ServiceChecker.
func NewServiceChecker(runner zfs.Runner, logger *slog.Logger) *ServiceChecker {
	return &ServiceChecker{
		runner: runner,
		logger: logger,
	}
}

// CheckServices checks the status of each service key. For each key, it tries
// candidate unit names in order. If no unit exists for a key, the key is
// silently skipped.
func (s *ServiceChecker) CheckServices(ctx context.Context, services map[string][]string) ([]ServiceStatus, error) {
	var statuses []ServiceStatus

	for key, units := range services {
		status, found := s.checkServiceUnits(ctx, key, units)
		if found {
			statuses = append(statuses, status)
		}
	}

	return statuses, nil
}

// checkServiceUnits tries each candidate unit name for a service key.
// Returns (status, true) if a unit was found, (zero, false) if none exist.
//
// Unit existence is determined via "systemctl show --property=LoadState <unit>".
// A unit with LoadState=not-found does not exist. This is reliable regardless
// of whether the unit is active, inactive, or failed -- unlike "systemctl
// is-active" which returns "inactive" with exit code 3 for both non-existent
// and genuinely stopped units.
func (s *ServiceChecker) checkServiceUnits(ctx context.Context, key string, units []string) (ServiceStatus, bool) {
	for _, unit := range units {
		if !s.unitExists(ctx, unit) {
			s.logger.Debug("unit not found, trying next", "key", key, "unit", unit)
			continue
		}

		// Unit exists -- check if it's active.
		out, err := s.runner(ctx, "systemctl", "is-active", unit)

		outStr := strings.TrimSpace(string(out))
		if err != nil && outStr == "" {
			// Command failed with no output -- treat as not active.
			s.logger.Debug("is-active failed with no output", "key", key, "unit", unit, "err", err)
			return ServiceStatus{Name: key, Active: false}, true
		}

		return ServiceStatus{Name: key, Active: outStr == "active"}, true
	}

	// No unit found for this key.
	s.logger.Debug("no unit found for service key, skipping", "key", key)

	return ServiceStatus{}, false
}

// unitExists checks whether a systemd unit is loaded (i.e. exists on disk).
// Uses "systemctl show --property=LoadState" which returns "not-found" for
// units that don't exist, regardless of active state.
func (s *ServiceChecker) unitExists(ctx context.Context, unit string) bool {
	out, err := s.runner(ctx, "systemctl", "show", "--property=LoadState", unit)
	if err != nil {
		s.logger.Debug("systemctl show failed", "unit", unit, "err", err)
		return false
	}

	return !strings.Contains(string(out), "not-found")
}
