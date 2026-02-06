package zfs

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Pool represents a ZFS storage pool.
type Pool struct {
	Name          string
	Size          uint64
	Allocated     uint64
	Free          uint64
	Fragmentation float64 // 0-1 ratio, NaN if unavailable
	DedupRatio    float64
	Health        string // ONLINE, DEGRADED, FAULTED, OFFLINE, REMOVED, UNAVAIL
	ReadOnly      bool
}

// poolColumns is the -o column list for zpool list.
const poolColumns = "name,size,alloc,free,frag,dedup,health,readonly"

// parsePools parses the output of: zpool list -Hp -o name,size,alloc,free,frag,dedup,health,readonly.
func parsePools(data []byte) ([]Pool, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}

	lines := strings.Split(trimmed, "\n")
	pools := make([]Pool, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 8 {
			return nil, fmt.Errorf("expected 8 fields, got %d: %q", len(fields), line)
		}

		pool, err := parsePoolFields(fields)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pool %q: %w", fields[0], err)
		}

		pools = append(pools, pool)
	}

	return pools, nil
}

func parsePoolFields(fields []string) (Pool, error) {
	size, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return Pool{}, fmt.Errorf("invalid size %q: %w", fields[1], err)
	}

	alloc, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return Pool{}, fmt.Errorf("invalid allocated %q: %w", fields[2], err)
	}

	free, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return Pool{}, fmt.Errorf("invalid free %q: %w", fields[3], err)
	}

	frag := math.NaN()
	if fields[4] != "-" {
		fragInt, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			return Pool{}, fmt.Errorf("invalid fragmentation %q: %w", fields[4], err)
		}

		frag = float64(fragInt) / 100.0
	}

	dedup, err := strconv.ParseFloat(fields[5], 64)
	if err != nil {
		return Pool{}, fmt.Errorf("invalid dedup ratio %q: %w", fields[5], err)
	}

	health := strings.ToUpper(fields[6])

	readonly := fields[7] == "on"

	return Pool{
		Name:          fields[0],
		Size:          size,
		Allocated:     alloc,
		Free:          free,
		Fragmentation: frag,
		DedupRatio:    dedup,
		Health:        health,
		ReadOnly:      readonly,
	}, nil
}
