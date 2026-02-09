package zfs

import (
	"regexp"
	"strconv"
	"strings"
)

// ScanStatus represents the current scan state for a pool.
type ScanStatus struct {
	Pool     string
	Scrub    bool    // true if scrub in progress
	Resilver bool    // true if resilver in progress
	Progress float64 // 0-1 scan progress, 0 if no active scan
}

var (
	// poolNameRe matches "pool: <name>" lines in zpool status output.
	poolNameRe = regexp.MustCompile(`^\s*pool:\s+(\S+)`)

	// scanActiveRe matches "scan: scrub in progress" or "scan: resilver in progress".
	scanActiveRe = regexp.MustCompile(`^\s*scan:\s+(scrub|resilver) in progress`)

	// progressRe matches percentage like "48.36% done".
	progressRe = regexp.MustCompile(`(\d+\.?\d*)%\s+done`)
)

// parseScanStatuses parses the output of: zpool status
// It splits by pool sections and extracts scan state for each pool.
func parseScanStatuses(data []byte) []ScanStatus {
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var statuses []ScanStatus

	var currentPool string

	var scanSeen bool

	for line := range strings.SplitSeq(text, "\n") {
		// Check for pool name — starts a new pool section.
		if m := poolNameRe.FindStringSubmatch(line); m != nil {
			// Close out previous pool if scan line was never seen.
			if currentPool != "" && !scanSeen {
				statuses = append(statuses, ScanStatus{Pool: currentPool})
			}

			currentPool = m[1]
			scanSeen = false

			continue
		}

		if currentPool == "" {
			continue
		}

		// Check for active scan line.
		if m := scanActiveRe.FindStringSubmatch(line); m != nil {
			scanSeen = true
			statuses = append(statuses, newActiveScan(currentPool, m[1]))

			continue
		}

		// Any other scan: line (none requested, completed, etc.) = no active scan.
		if strings.Contains(line, "scan:") {
			scanSeen = true
			statuses = append(statuses, ScanStatus{Pool: currentPool})

			continue
		}

		// Extract progress percentage from lines following an active scan.
		tryParseProgress(&statuses, currentPool, line)
	}

	// Close out last pool if scan was never seen.
	if currentPool != "" && !scanSeen {
		statuses = append(statuses, ScanStatus{Pool: currentPool})
	}

	return statuses
}

// newActiveScan builds a ScanStatus for an active scrub or resilver.
func newActiveScan(pool, scanType string) ScanStatus {
	status := ScanStatus{Pool: pool}

	switch scanType {
	case "scrub":
		status.Scrub = true
	case "resilver":
		status.Resilver = true
	}

	return status
}

// tryParseProgress extracts progress percentage from a line and updates the last status.
func tryParseProgress(statuses *[]ScanStatus, currentPool, line string) {
	if len(*statuses) == 0 {
		return
	}

	last := &(*statuses)[len(*statuses)-1]
	if last.Pool != currentPool || (!last.Scrub && !last.Resilver) || last.Progress != 0 {
		return
	}

	if m := progressRe.FindStringSubmatch(line); m != nil {
		pct, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			last.Progress = pct / 100.0
		}
	}
}
