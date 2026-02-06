package config

import "errors"

// Sentinel errors for configuration validation.
var (
	ErrZpoolNotFound = errors.New("zpool binary not found or not executable")
	ErrZfsNotFound   = errors.New("zfs binary not found or not executable")
)
