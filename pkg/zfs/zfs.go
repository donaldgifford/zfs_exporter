// Package zfs executes ZFS CLI commands and parses their output into typed
// structs for consumption by a Prometheus collector. No libzfs, no CGo.
package zfs

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

// Runner executes a command and returns stdout.
// Production: wraps exec.CommandContext.
// Tests: returns fixture data.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner returns a Runner that uses exec.CommandContext.
func DefaultRunner() Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := exec.CommandContext(ctx, name, args...).Output()
		if err != nil {
			return nil, fmt.Errorf("command %q failed: %w", name, err)
		}

		return out, nil
	}
}

// Client executes ZFS CLI commands and parses their output.
type Client struct {
	runner    Runner
	logger    *slog.Logger
	zpoolPath string
	zfsPath   string
}

// NewClient creates a Client with the given runner, logger, and binary paths.
func NewClient(runner Runner, logger *slog.Logger, zpoolPath, zfsPath string) *Client {
	return &Client{
		runner:    runner,
		logger:    logger,
		zpoolPath: zpoolPath,
		zfsPath:   zfsPath,
	}
}

// GetPools returns all ZFS pools.
func (c *Client) GetPools(ctx context.Context) ([]Pool, error) {
	out, err := c.runner(ctx, c.zpoolPath, "list", "-Hp", "-o", poolColumns)
	if err != nil {
		return nil, fmt.Errorf("zpool list failed: %w", err)
	}

	pools, err := parsePools(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pool output: %w", err)
	}

	return pools, nil
}

// GetDatasets returns all ZFS datasets (filesystems and volumes).
func (c *Client) GetDatasets(ctx context.Context) ([]Dataset, error) {
	out, err := c.runner(ctx, c.zfsPath, "list", "-Hp", "-o", datasetColumns, "-t", "filesystem,volume")
	if err != nil {
		return nil, fmt.Errorf("zfs list failed: %w", err)
	}

	datasets, err := parseDatasets(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dataset output: %w", err)
	}

	return datasets, nil
}

// GetScanStatuses returns the scan status for all pools.
func (c *Client) GetScanStatuses(ctx context.Context) ([]ScanStatus, error) {
	out, err := c.runner(ctx, c.zpoolPath, "status")
	if err != nil {
		return nil, fmt.Errorf("zpool status failed: %w", err)
	}

	return parseScanStatuses(out), nil
}
