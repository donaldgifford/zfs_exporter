#!/bin/sh
set -e

# Stop and disable the service before removal
systemctl stop zfs_exporter.service || true
systemctl disable zfs_exporter.service || true
systemctl daemon-reload
