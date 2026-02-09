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
	"iscsi": {"tgt.service", "iscsitarget.service"},
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
func (s *ServiceChecker) checkServiceUnits(ctx context.Context, key string, units []string) (ServiceStatus, bool) {
	for _, unit := range units {
		out, err := s.runner(ctx, "systemctl", "is-active", unit)
		if err != nil {
			// "systemctl is-active" returns non-zero for inactive/failed, which
			// exec wraps as an error. Check if we got output anyway.
			outStr := strings.TrimSpace(string(out))
			if outStr == "" {
				// No output at all likely means the unit doesn't exist. Try next.
				s.logger.Debug("unit not found, trying next", "key", key, "unit", unit)
				continue
			}

			// Got output (e.g. "inactive", "failed") — unit exists but isn't active.
			return ServiceStatus{Name: key, Active: false}, true
		}

		outStr := strings.TrimSpace(string(out))

		return ServiceStatus{Name: key, Active: outStr == "active"}, true
	}

	// No unit found for this key.
	s.logger.Debug("no unit found for service key, skipping", "key", key)

	return ServiceStatus{}, false
}
