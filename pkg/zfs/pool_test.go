package zfs

import (
	"math"
	"testing"
)

func TestParsePools(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPools []Pool
		wantErr   bool
	}{
		{
			name:  "single pool",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    1.00,
					Health:        "ONLINE",
					ReadOnly:      false,
				},
			},
		},
		{
			name: "multiple pools",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n" +
				"backup\t5368709120\t1073741824\t4294967296\t10\t1.00\tONLINE\toff\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    1.00,
					Health:        "ONLINE",
					ReadOnly:      false,
				},
				{
					Name:          "backup",
					Size:          5368709120,
					Allocated:     1073741824,
					Free:          4294967296,
					Fragmentation: 0.10,
					DedupRatio:    1.00,
					Health:        "ONLINE",
					ReadOnly:      false,
				},
			},
		},
		{
			name:  "fragmentation unavailable",
			input: "backup\t5368709120\t1073741824\t4294967296\t-\t1.00\tONLINE\toff\n",
			wantPools: []Pool{
				{
					Name:          "backup",
					Size:          5368709120,
					Allocated:     1073741824,
					Free:          4294967296,
					Fragmentation: math.NaN(),
					DedupRatio:    1.00,
					Health:        "ONLINE",
					ReadOnly:      false,
				},
			},
		},
		{
			name:  "read-only pool",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\ton\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    1.00,
					Health:        "ONLINE",
					ReadOnly:      true,
				},
			},
		},
		{
			name:  "degraded pool",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tDEGRADED\toff\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    1.00,
					Health:        "DEGRADED",
					ReadOnly:      false,
				},
			},
		},
		{
			name:  "faulted pool",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tFAULTED\toff\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    1.00,
					Health:        "FAULTED",
					ReadOnly:      false,
				},
			},
		},
		{
			name:  "dedup ratio greater than 1",
			input: "tank\t10737418240\t5368709120\t5368709120\t33\t2.50\tONLINE\toff\n",
			wantPools: []Pool{
				{
					Name:          "tank",
					Size:          10737418240,
					Allocated:     5368709120,
					Free:          5368709120,
					Fragmentation: 0.33,
					DedupRatio:    2.50,
					Health:        "ONLINE",
					ReadOnly:      false,
				},
			},
		},
		{
			name:      "empty output",
			input:     "",
			wantPools: nil,
		},
		{
			name:      "whitespace only",
			input:     "  \n  \n",
			wantPools: nil,
		},
		{
			name:    "wrong field count",
			input:   "tank\t10737418240\t5368709120\n",
			wantErr: true,
		},
		{
			name:    "invalid size",
			input:   "tank\tnotanumber\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid allocated",
			input:   "tank\t10737418240\tnotanumber\t5368709120\t33\t1.00\tONLINE\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid free",
			input:   "tank\t10737418240\t5368709120\tnotanumber\t33\t1.00\tONLINE\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid fragmentation",
			input:   "tank\t10737418240\t5368709120\t5368709120\tnotanumber\t1.00\tONLINE\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid dedup",
			input:   "tank\t10737418240\t5368709120\t5368709120\t33\tnotanumber\tONLINE\toff\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools, err := parsePools([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePools() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if len(pools) != len(tt.wantPools) {
				t.Fatalf("got %d pools, want %d", len(pools), len(tt.wantPools))
			}

			for i, got := range pools {
				want := tt.wantPools[i]
				if got.Name != want.Name {
					t.Errorf("pool[%d].Name = %q, want %q", i, got.Name, want.Name)
				}

				if got.Size != want.Size {
					t.Errorf("pool[%d].Size = %d, want %d", i, got.Size, want.Size)
				}

				if got.Allocated != want.Allocated {
					t.Errorf("pool[%d].Allocated = %d, want %d", i, got.Allocated, want.Allocated)
				}

				if got.Free != want.Free {
					t.Errorf("pool[%d].Free = %d, want %d", i, got.Free, want.Free)
				}

				if math.IsNaN(want.Fragmentation) {
					if !math.IsNaN(got.Fragmentation) {
						t.Errorf("pool[%d].Fragmentation = %f, want NaN", i, got.Fragmentation)
					}
				} else if got.Fragmentation != want.Fragmentation {
					t.Errorf("pool[%d].Fragmentation = %f, want %f", i, got.Fragmentation, want.Fragmentation)
				}

				if got.DedupRatio != want.DedupRatio {
					t.Errorf("pool[%d].DedupRatio = %f, want %f", i, got.DedupRatio, want.DedupRatio)
				}

				if got.Health != want.Health {
					t.Errorf("pool[%d].Health = %q, want %q", i, got.Health, want.Health)
				}

				if got.ReadOnly != want.ReadOnly {
					t.Errorf("pool[%d].ReadOnly = %v, want %v", i, got.ReadOnly, want.ReadOnly)
				}
			}
		})
	}
}
