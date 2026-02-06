package zfs

import (
	"fmt"
	"strconv"
	"strings"
)

// Dataset represents a ZFS dataset (filesystem or volume).
type Dataset struct {
	Name       string
	Pool       string // extracted from Name: "tank/data" -> "tank"
	Used       uint64
	Available  uint64
	Referenced uint64
	Type       string // "filesystem" or "volume"
	ShareNFS   bool   // true if sharenfs != "off" and != "-"
	ShareSMB   bool   // true if sharesmb != "off" and != "-"
}

// datasetColumns is the -o column list for zfs list.
const datasetColumns = "name,used,avail,refer,type,sharenfs,sharesmb"

// parseDatasets parses the output of:
// zfs list -Hp -o name,used,avail,refer,type,sharenfs,sharesmb -t filesystem,volume.
func parseDatasets(data []byte) ([]Dataset, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}

	lines := strings.Split(trimmed, "\n")
	datasets := make([]Dataset, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return nil, fmt.Errorf("expected 7 fields, got %d: %q", len(fields), line)
		}

		ds, err := parseDatasetFields(fields)
		if err != nil {
			return nil, fmt.Errorf("failed to parse dataset %q: %w", fields[0], err)
		}

		datasets = append(datasets, ds)
	}

	return datasets, nil
}

func parseDatasetFields(fields []string) (Dataset, error) {
	used, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return Dataset{}, fmt.Errorf("invalid used %q: %w", fields[1], err)
	}

	avail, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return Dataset{}, fmt.Errorf("invalid available %q: %w", fields[2], err)
	}

	ref, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return Dataset{}, fmt.Errorf("invalid referenced %q: %w", fields[3], err)
	}

	return Dataset{
		Name:       fields[0],
		Pool:       extractPool(fields[0]),
		Used:       used,
		Available:  avail,
		Referenced: ref,
		Type:       fields[4],
		ShareNFS:   isShareEnabled(fields[5]),
		ShareSMB:   isShareEnabled(fields[6]),
	}, nil
}

// extractPool returns the pool name from a dataset path.
// "tank/data/photos" -> "tank", "tank" -> "tank".
func extractPool(name string) string {
	pool, _, found := strings.Cut(name, "/")
	if found {
		return pool
	}

	return name
}

// isShareEnabled returns true if the share property value indicates sharing is enabled.
// "off" and "-" (volumes) mean not shared; anything else means shared.
func isShareEnabled(val string) bool {
	return val != "off" && val != "-"
}
