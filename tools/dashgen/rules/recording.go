package rules

// RecordingRules generates the recording rules for anomaly detection baselines.
// These rules are static (not service-dependent).
func RecordingRules() RuleFile {
	return RuleFile{
		Groups: []RuleGroup{
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
		},
	}
}
