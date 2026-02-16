package rules

// recordingRuleGroups returns the rule groups for anomaly detection baselines.
// These rules are static (not service-dependent).
func recordingRuleGroups() []RuleGroup {
	return []RuleGroup{
		{
			Name:     "zfs_anomaly_baselines",
			Interval: "5m",
			Rules: []Rule{
				{
					Record: "zfs:dataset_used_bytes:avg1d",
					Expr:   "avg_over_time(zfs_dataset_used_bytes[1d])",
				},
				{
					Record: "zfs:dataset_used_bytes:stddev1d",
					Expr:   "stddev_over_time(zfs_dataset_used_bytes[1d])",
				},
				{
					Record: "zfs:dataset_used_bytes:avg7d",
					Expr:   "avg_over_time(zfs_dataset_used_bytes[7d])",
				},
				{
					Record: "zfs:dataset_used_bytes:stddev7d",
					Expr:   "stddev_over_time(zfs_dataset_used_bytes[7d])",
				},
				{
					Record: "zfs:dataset_used_bytes:deriv1h",
					Expr:   "deriv(zfs_dataset_used_bytes[1h])",
				},
			},
		},
	}
}

// RecordingRules generates the recording rules as a raw Prometheus RuleFile.
func RecordingRules() RuleFile {
	return RuleFile{Groups: recordingRuleGroups()}
}

// RecordingPrometheusRule generates the recording rules wrapped in a
// Kubernetes PrometheusRule CR.
func RecordingPrometheusRule() PrometheusRule {
	return PrometheusRule{
		APIVersion: "monitoring.coreos.com/v1",
		Kind:       "PrometheusRule",
		Metadata: PrometheusRuleMetadata{
			Name:      "zfs-recording-rules",
			Namespace: "monitoring",
			Labels: map[string]string{
				"prometheus": "system-rules-prometheus",
			},
		},
		Spec: PrometheusRuleSpec{Groups: recordingRuleGroups()},
	}
}
