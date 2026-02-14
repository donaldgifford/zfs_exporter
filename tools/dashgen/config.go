package main

import (
	"errors"
	"fmt"
)

// ServiceConfig defines a service whose panels appear in generated dashboards.
type ServiceConfig struct {
	// Key is the service identifier used in metrics (e.g. "nfs", "smb", "iscsi").
	Key string

	// Label is the display name in dashboard panels (e.g. "NFS", "SMB", "iSCSI").
	Label string

	// ShareMetric is the metric name for share detection.
	// For NFS: "zfs_dataset_share_nfs", for SMB: "zfs_dataset_share_smb".
	// Empty means this service does not use share metrics.
	ShareMetric string

	// UseZvols indicates this service should show zvol inventory instead of
	// share datasets (true for iSCSI).
	UseZvols bool
}

// DashboardSet controls which dashboards to generate.
type DashboardSet struct {
	Status   bool // zfs-status.json
	Details  bool // zfs-details.json
	Combined bool // zfs-combined.json
}

// Config defines what the dashboard generator produces.
type Config struct {
	// Services to include in dashboards. Only listed services get panels.
	Services []ServiceConfig

	// Dashboards to generate.
	Dashboards DashboardSet

	// OutputDir is the directory to write JSON files.
	OutputDir string
}

// DefaultConfig generates all dashboards with all services enabled.
var DefaultConfig = Config{
	Services: []ServiceConfig{
		{Key: "nfs", Label: "NFS", ShareMetric: "zfs_dataset_share_nfs"},
		{Key: "smb", Label: "SMB", ShareMetric: "zfs_dataset_share_smb"},
		{Key: "iscsi", Label: "iSCSI", UseZvols: true},
	},
	Dashboards: DashboardSet{Status: true, Details: true, Combined: true},
	OutputDir:  "contrib/grafana",
}

// Validate checks the config for errors.
func (c *Config) Validate() error {
	var errs []error

	if len(c.Services) == 0 {
		errs = append(errs, errors.New("at least one service is required"))
	}

	seen := make(map[string]bool, len(c.Services))

	for i, svc := range c.Services {
		if svc.Key == "" {
			errs = append(errs, fmt.Errorf("service[%d]: key is required", i))
		}

		if svc.Label == "" {
			errs = append(errs, fmt.Errorf("service[%d]: label is required", i))
		}

		if svc.ShareMetric == "" && !svc.UseZvols {
			errs = append(errs, fmt.Errorf("service %q: must set either share_metric or use_zvols", svc.Key))
		}

		if svc.ShareMetric != "" && svc.UseZvols {
			errs = append(errs, fmt.Errorf("service %q: cannot set both share_metric and use_zvols", svc.Key))
		}

		if seen[svc.Key] {
			errs = append(errs, fmt.Errorf("service %q: duplicate key", svc.Key))
		}

		seen[svc.Key] = true
	}

	if c.OutputDir == "" {
		errs = append(errs, errors.New("output_dir is required"))
	}

	if !c.Dashboards.Status && !c.Dashboards.Details && !c.Dashboards.Combined {
		errs = append(errs, errors.New("at least one dashboard must be enabled"))
	}

	return errors.Join(errs...)
}
