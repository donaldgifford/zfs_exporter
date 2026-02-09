package zfs

import (
	"testing"
)

func TestParseDatasets(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantDatasets []Dataset
		wantErr      bool
	}{
		{
			name: "mixed filesystems and volumes",
			input: "tank\t5368709120\t5368709120\t262144\tfilesystem\toff\toff\n" +
				"tank/media\t4294967296\t5368709120\t4294967296\tfilesystem\ton\toff\n" +
				"tank/backups\t1073741824\t5368709120\t1073741824\tfilesystem\trw=@10.0.0.0/24\toff\n" +
				"tank/shared\t536870912\t5368709120\t536870912\tfilesystem\toff\ton\n" +
				"tank/zvol0\t1073741824\t5368709120\t1073741824\tvolume\t-\t-\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank",
					Pool:       "tank",
					Used:       5368709120,
					Available:  5368709120,
					Referenced: 262144,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
				{
					Name:       "tank/media",
					Pool:       "tank",
					Used:       4294967296,
					Available:  5368709120,
					Referenced: 4294967296,
					Type:       "filesystem",
					ShareNFS:   true,
					ShareSMB:   false,
				},
				{
					Name:       "tank/backups",
					Pool:       "tank",
					Used:       1073741824,
					Available:  5368709120,
					Referenced: 1073741824,
					Type:       "filesystem",
					ShareNFS:   true,
					ShareSMB:   false,
				},
				{
					Name:       "tank/shared",
					Pool:       "tank",
					Used:       536870912,
					Available:  5368709120,
					Referenced: 536870912,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   true,
				},
				{
					Name:       "tank/zvol0",
					Pool:       "tank",
					Used:       1073741824,
					Available:  5368709120,
					Referenced: 1073741824,
					Type:       "volume",
					ShareNFS:   false,
					ShareSMB:   false,
				},
			},
		},
		{
			name:  "single root dataset",
			input: "tank\t262144\t5368709120\t262144\tfilesystem\toff\toff\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank",
					Pool:       "tank",
					Used:       262144,
					Available:  5368709120,
					Referenced: 262144,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
			},
		},
		{
			name:  "deeply nested dataset",
			input: "tank/data/photos/2025\t1073741824\t5368709120\t1073741824\tfilesystem\toff\toff\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank/data/photos/2025",
					Pool:       "tank",
					Used:       1073741824,
					Available:  5368709120,
					Referenced: 1073741824,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
			},
		},
		{
			name:  "sharenfs with options string",
			input: "tank/exports\t1073741824\t5368709120\t1073741824\tfilesystem\trw=@10.0.0.0/24,ro=@192.168.1.0/24\toff\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank/exports",
					Pool:       "tank",
					Used:       1073741824,
					Available:  5368709120,
					Referenced: 1073741824,
					Type:       "filesystem",
					ShareNFS:   true,
					ShareSMB:   false,
				},
			},
		},
		{
			name:  "both NFS and SMB enabled",
			input: "tank/shared\t536870912\t5368709120\t536870912\tfilesystem\ton\ton\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank/shared",
					Pool:       "tank",
					Used:       536870912,
					Available:  5368709120,
					Referenced: 536870912,
					Type:       "filesystem",
					ShareNFS:   true,
					ShareSMB:   true,
				},
			},
		},
		{
			name: "multiple pools",
			input: "tank\t5368709120\t5368709120\t262144\tfilesystem\toff\toff\n" +
				"backup\t1073741824\t4294967296\t262144\tfilesystem\toff\toff\n" +
				"backup/daily\t536870912\t4294967296\t536870912\tfilesystem\toff\toff\n",
			wantDatasets: []Dataset{
				{
					Name:       "tank",
					Pool:       "tank",
					Used:       5368709120,
					Available:  5368709120,
					Referenced: 262144,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
				{
					Name:       "backup",
					Pool:       "backup",
					Used:       1073741824,
					Available:  4294967296,
					Referenced: 262144,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
				{
					Name:       "backup/daily",
					Pool:       "backup",
					Used:       536870912,
					Available:  4294967296,
					Referenced: 536870912,
					Type:       "filesystem",
					ShareNFS:   false,
					ShareSMB:   false,
				},
			},
		},
		{
			name:         "empty output",
			input:        "",
			wantDatasets: nil,
		},
		{
			name:    "wrong field count",
			input:   "tank\t5368709120\t5368709120\n",
			wantErr: true,
		},
		{
			name:    "invalid used",
			input:   "tank\tnotanumber\t5368709120\t262144\tfilesystem\toff\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid available",
			input:   "tank\t5368709120\tnotanumber\t262144\tfilesystem\toff\toff\n",
			wantErr: true,
		},
		{
			name:    "invalid referenced",
			input:   "tank\t5368709120\t5368709120\tnotanumber\tfilesystem\toff\toff\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			datasets, err := parseDatasets([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDatasets() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if len(datasets) != len(tt.wantDatasets) {
				t.Fatalf("got %d datasets, want %d", len(datasets), len(tt.wantDatasets))
			}

			for i, got := range datasets {
				want := tt.wantDatasets[i]
				if got.Name != want.Name {
					t.Errorf("dataset[%d].Name = %q, want %q", i, got.Name, want.Name)
				}

				if got.Pool != want.Pool {
					t.Errorf("dataset[%d].Pool = %q, want %q", i, got.Pool, want.Pool)
				}

				if got.Used != want.Used {
					t.Errorf("dataset[%d].Used = %d, want %d", i, got.Used, want.Used)
				}

				if got.Available != want.Available {
					t.Errorf("dataset[%d].Available = %d, want %d", i, got.Available, want.Available)
				}

				if got.Referenced != want.Referenced {
					t.Errorf("dataset[%d].Referenced = %d, want %d", i, got.Referenced, want.Referenced)
				}

				if got.Type != want.Type {
					t.Errorf("dataset[%d].Type = %q, want %q", i, got.Type, want.Type)
				}

				if got.ShareNFS != want.ShareNFS {
					t.Errorf("dataset[%d].ShareNFS = %v, want %v", i, got.ShareNFS, want.ShareNFS)
				}

				if got.ShareSMB != want.ShareSMB {
					t.Errorf("dataset[%d].ShareSMB = %v, want %v", i, got.ShareSMB, want.ShareSMB)
				}
			}
		})
	}
}

func TestExtractPool(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"tank", "tank"},
		{"tank/data", "tank"},
		{"tank/data/photos", "tank"},
		{"rpool/ROOT/ubuntu", "rpool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPool(tt.name); got != tt.want {
				t.Errorf("extractPool(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsShareEnabled(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"off", false},
		{"-", false},
		{"on", true},
		{"rw=@10.0.0.0/24", true},
		{"rw=@10.0.0.0/24,ro=@192.168.1.0/24", true},
	}

	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			if got := isShareEnabled(tt.val); got != tt.want {
				t.Errorf("isShareEnabled(%q) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}
