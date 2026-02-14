package rules

import "fmt"

// AlertRules generates alert rules. Service-specific mismatch alerts are only
// generated for services with a ShareMetric configured.
func AlertRules(services []ServiceConfig) RuleFile {
	rules := []Rule{
		// Exporter health.
		{
			Alert:  "ZfsExporterDown",
			Expr:   `up{job="zfs_exporter"} == 0`,
			For:    "5m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": "ZFS exporter is down on {{ $labels.instance }}",
			},
		},
		{
			Alert:  "ZfsCommandFailure",
			Expr:   "zfs_up == 0",
			For:    "2m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": "ZFS commands failing on {{ $labels.instance }}",
			},
		},
		// Drive failure and rebuild.
		{
			Alert:  "ZfsPoolDegraded",
			Expr:   `zfs_pool_health{state="degraded"} == 1`,
			For:    "1m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary":     "ZFS pool {{ $labels.pool }} is DEGRADED (drive failure)",
				"description": "A vdev in pool {{ $labels.pool }} has failed. Check zpool status for details.",
			},
		},
		{
			Alert:  "ZfsPoolFaulted",
			Expr:   `zfs_pool_health{state="faulted"} == 1`,
			For:    "0m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary":     "ZFS pool {{ $labels.pool }} is FAULTED",
				"description": "Pool {{ $labels.pool }} has experienced too many failures and is no longer accessible.",
			},
		},
		{
			Alert: "ZfsPoolDegradedNotResilvering",
			Expr: `(zfs_pool_health{state="degraded"} == 1)
  unless on(pool)
(zfs_pool_resilver_active == 1)`,
			For:    "10m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary":     "Pool {{ $labels.pool }} is degraded but NOT resilvering",
				"description": "A drive has failed in pool {{ $labels.pool }} and no resilver is in progress. Manual intervention required.",
			},
		},
		{
			Alert:  "ZfsPoolResilvering",
			Expr:   "zfs_pool_resilver_active == 1",
			For:    "0m",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary":     "Pool {{ $labels.pool }} resilver in progress ({{ $value | humanizePercentage }} complete)",
				"description": "A drive rebuild is underway for pool {{ $labels.pool }}.",
			},
		},
		{
			Alert: "ZfsPoolResilverStalled",
			Expr: `(zfs_pool_resilver_active == 1)
  and
(delta(zfs_pool_scan_progress_ratio[30m]) == 0)`,
			For:    "30m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary":     "Resilver stalled on pool {{ $labels.pool }}",
				"description": "Resilver progress has not advanced in 30 minutes.",
			},
		},
		// Pool health catch-all.
		{
			Alert: "ZfsPoolNotOnline",
			Expr: `(zfs_pool_health{state="online"} == 0)
  unless on(pool)
(zfs_pool_health{state="degraded"} == 1)
  unless on(pool)
(zfs_pool_health{state="faulted"} == 1)`,
			For:    "1m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": "ZFS pool {{ $labels.pool }} is not ONLINE",
			},
		},
		{
			Alert:  "ZfsPoolReadOnly",
			Expr:   "zfs_pool_readonly == 1",
			For:    "1m",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary": "ZFS pool {{ $labels.pool }} is read-only",
			},
		},
		// Capacity.
		{
			Alert:  "ZfsPoolCapacityWarning",
			Expr:   "(zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.80",
			For:    "15m",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary": "ZFS pool {{ $labels.pool }} is {{ $value | humanizePercentage }} full",
			},
		},
		{
			Alert:  "ZfsPoolCapacityCritical",
			Expr:   "(zfs_pool_allocated_bytes / zfs_pool_size_bytes) > 0.90",
			For:    "5m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": "ZFS pool {{ $labels.pool }} is {{ $value | humanizePercentage }} full",
			},
		},
		{
			Alert:  "ZfsPoolFragmentationHigh",
			Expr:   "zfs_pool_fragmentation_ratio > 0.50",
			For:    "1h",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary": "ZFS pool {{ $labels.pool }} fragmentation is {{ $value | humanizePercentage }}",
			},
		},
		// Service down (generic, applies to all configured services).
		{
			Alert:  "ZfsServiceDown",
			Expr:   "zfs_service_up == 0",
			For:    "2m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": "Service {{ $labels.service }} is down on {{ $labels.instance }}",
			},
		},
	}

	// Per-service share/service mismatch alerts.
	for _, svc := range services {
		if svc.ShareMetric == "" {
			continue
		}
		rules = append(rules, Rule{
			Alert: fmt.Sprintf("Zfs%sSharesWithoutService", svc.Label),
			Expr: fmt.Sprintf(`(count(%s == 1) > 0)
  and
(zfs_service_up{service="%s"} == 0)`, svc.ShareMetric, svc.Key),
			For:    "2m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary": fmt.Sprintf("%s shares configured but %s service is down on {{ $labels.instance }}", svc.Label, svc.Label),
			},
		})
	}

	// Anomaly detection alerts.
	rules = append(rules,
		Rule{
			Alert: "ZfsDatasetAbnormalGrowth",
			Expr: `(
  (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg7d)
    > 2 * zfs:dataset_used_bytes:stddev7d
)
and
(
  (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg7d)
    > max(1073741824, 0.1 * zfs:dataset_used_bytes:avg7d)
)`,
			For:    "1h",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary":     "Dataset {{ $labels.dataset }} usage is outside normal 7-day range",
				"description": "Current usage has deviated more than 2 standard deviations from the 7-day average and exceeds the minimum threshold floor.",
			},
		},
		Rule{
			Alert: "ZfsDatasetAbnormalGrowthShortTerm",
			Expr: `(
  (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg1d)
    > 3 * zfs:dataset_used_bytes:stddev1d
)
and
(
  (zfs_dataset_used_bytes - zfs:dataset_used_bytes:avg1d)
    > max(1073741824, 0.1 * zfs:dataset_used_bytes:avg1d)
)`,
			For:    "30m",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary":     "Dataset {{ $labels.dataset }} usage spiking beyond 1-day baseline",
				"description": "Current usage has deviated more than 3 standard deviations from the 1-day average and exceeds the minimum threshold floor.",
			},
		},
		Rule{
			Alert:  "ZfsPoolPredictedFull7d",
			Expr:   "predict_linear(zfs_pool_free_bytes[7d], 7 * 24 * 3600) < 0",
			For:    "1h",
			Labels: map[string]string{"severity": "warning"},
			Annotations: map[string]string{
				"summary":     "Pool {{ $labels.pool }} predicted to fill within 7 days",
				"description": "Based on 7-day growth trend, pool {{ $labels.pool }} will run out of space.",
			},
		},
		Rule{
			Alert:  "ZfsPoolPredictedFull1d",
			Expr:   "predict_linear(zfs_pool_free_bytes[1d], 24 * 3600) < 0",
			For:    "30m",
			Labels: map[string]string{"severity": "critical"},
			Annotations: map[string]string{
				"summary":     "Pool {{ $labels.pool }} predicted to fill within 24 hours",
				"description": "Based on 1-day growth trend, pool {{ $labels.pool }} will run out of space imminently.",
			},
		},
	)

	return RuleFile{
		Groups: []RuleGroup{
			{
				Name:  "zfs_exporter",
				Rules: rules,
			},
		},
	}
}
