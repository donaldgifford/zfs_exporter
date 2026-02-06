#!/bin/sh
set -e

# Create system user if it doesn't exist
if ! getent passwd zfs_exporter >/dev/null 2>&1; then
    adduser --system --group --no-create-home --shell /usr/sbin/nologin zfs_exporter
fi

# Reload systemd and enable the service
systemctl daemon-reload
systemctl enable zfs_exporter.service

# Start or restart the service
if systemctl is-active --quiet zfs_exporter.service; then
    systemctl restart zfs_exporter.service
else
    systemctl start zfs_exporter.service
fi
