package zfs

import (
	"math"
	"testing"
)

func floatClose(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestParseScanStatuses(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []ScanStatus
	}{
		{
			name: "no scan requested",
			input: `  pool: tank
 state: ONLINE
  scan: none requested
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: false, Resilver: false, Progress: 0},
			},
		},
		{
			name: "scrub in progress",
			input: `  pool: tank
 state: ONLINE
  scan: scrub in progress since Sun Jul 25 16:07:49 2025
    374G scanned at 161M/s, 340G issued at 146M/s, 703G total
    0B repaired, 48.36% done, 00:42:27 to go
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: true, Resilver: false, Progress: 0.4836},
			},
		},
		{
			name: "resilver in progress",
			input: `  pool: tank
 state: DEGRADED
  scan: resilver in progress since Mon Feb  3 10:00:00 2025
    1.23G scanned at 100M/s, 500M issued at 50M/s, 5.00G total
    500M resilvered, 10.00% done, 0 days 01:30:00 to go
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: false, Resilver: true, Progress: 0.10},
			},
		},
		{
			name: "completed scrub",
			input: `  pool: tank
 state: ONLINE
  scan: scrub repaired 0B in 01:23:45 with 0 errors on Sun Feb  2 00:24:01 2025
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: false, Resilver: false, Progress: 0},
			},
		},
		{
			name: "multiple pools different states",
			input: `  pool: tank
 state: ONLINE
  scan: scrub in progress since Sun Jul 25 16:07:49 2025
    374G scanned at 161M/s, 340G issued at 146M/s, 703G total
    0B repaired, 48.36% done, 00:42:27 to go

  pool: backup
 state: ONLINE
  scan: none requested
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: true, Resilver: false, Progress: 0.4836},
				{Pool: "backup", Scrub: false, Resilver: false, Progress: 0},
			},
		},
		{
			name: "pool with resilver and another online",
			input: `  pool: tank
 state: DEGRADED
  scan: resilver in progress since Mon Feb  3 10:00:00 2025
    1.23G scanned at 100M/s, 500M issued at 50M/s, 5.00G total
    500M resilvered, 75.50% done, 0 days 00:30:00 to go

  pool: backup
 state: ONLINE
  scan: scrub repaired 0B in 01:23:45 with 0 errors on Sun Feb  2 00:24:01 2025
`,
			want: []ScanStatus{
				{Pool: "tank", Scrub: false, Resilver: true, Progress: 0.755},
				{Pool: "backup", Scrub: false, Resilver: false, Progress: 0},
			},
		},
		{
			name:  "empty output",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   \n  \n",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScanStatuses([]byte(tt.input))

			if len(got) != len(tt.want) {
				t.Fatalf("got %d statuses, want %d", len(got), len(tt.want))
			}

			for i, g := range got {
				w := tt.want[i]
				if g.Pool != w.Pool {
					t.Errorf("[%d].Pool = %q, want %q", i, g.Pool, w.Pool)
				}

				if g.Scrub != w.Scrub {
					t.Errorf("[%d].Scrub = %v, want %v", i, g.Scrub, w.Scrub)
				}

				if g.Resilver != w.Resilver {
					t.Errorf("[%d].Resilver = %v, want %v", i, g.Resilver, w.Resilver)
				}

				if !floatClose(g.Progress, w.Progress, 0.001) {
					t.Errorf("[%d].Progress = %f, want %f", i, g.Progress, w.Progress)
				}
			}
		})
	}
}
