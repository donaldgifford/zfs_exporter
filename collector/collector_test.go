package collector

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/donaldgifford/zfs_exporter/pkg/host"
	"github.com/donaldgifford/zfs_exporter/pkg/zfs"
)

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, nil))
}

// fixtureRunner dispatches by command name to return test fixture data.
type fixtureRunner struct {
	poolOut    string
	poolErr    error
	datasetOut string
	datasetErr error
	statusOut  string
	statusErr  error
	svcResults map[string]struct {
		output string
		err    error
	}
}

func (f *fixtureRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	// Determine which command was called.
	switch {
	case strings.HasSuffix(name, "zpool") && len(args) > 0 && args[0] == "list":
		return []byte(f.poolOut), f.poolErr
	case strings.HasSuffix(name, "zfs") && len(args) > 0 && args[0] == "list":
		return []byte(f.datasetOut), f.datasetErr
	case strings.HasSuffix(name, "zpool") && len(args) > 0 && args[0] == "status":
		return []byte(f.statusOut), f.statusErr
	case name == "systemctl":
		if f.svcResults == nil {
			return []byte(""), errors.New("no service results configured")
		}

		unit := args[len(args)-1]
		if r, ok := f.svcResults[unit]; ok {
			return []byte(r.output), r.err
		}

		return []byte(""), errors.New("unit not found")
	default:
		return []byte(""), nil
	}
}

func newTestCollector(f *fixtureRunner) *Collector {
	client := zfs.NewClient(f.run, testLogger(), "zpool", "zfs")
	svcChecker := host.NewServiceChecker(f.run, testLogger())

	services := map[string][]string{
		"nfs": {"nfs-kernel-server.service"},
		"smb": {"smbd.service"},
	}

	return NewCollector(client, svcChecker, testLogger(), 10*time.Second, services)
}

func TestCollector_HappyPath(t *testing.T) {
	f := &fixtureRunner{
		poolOut:    "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n",
		datasetOut: "tank\t5368709120\t5368709120\t262144\tfilesystem\toff\toff\ntank/media\t4294967296\t5368709120\t4294967296\tfilesystem\ton\toff\n",
		statusOut: `  pool: tank
 state: ONLINE
  scan: none requested
`,
		svcResults: map[string]struct {
			output string
			err    error
		}{
			"nfs-kernel-server.service": {"active\n", nil},
			"smbd.service":              {"active\n", nil},
		},
	}

	coll := newTestCollector(f)

	// Verify pool size metric.
	expected := `
		# HELP zfs_pool_size_bytes Total pool size in bytes.
		# TYPE zfs_pool_size_bytes gauge
		zfs_pool_size_bytes{pool="tank"} 1.073741824e+10
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(expected), "zfs_pool_size_bytes"); err != nil {
		t.Errorf("pool size mismatch: %v", err)
	}

	// Verify up metric.
	upExpected := `
		# HELP zfs_up Whether ZFS commands succeeded.
		# TYPE zfs_up gauge
		zfs_up 1
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(upExpected), "zfs_up"); err != nil {
		t.Errorf("up metric mismatch: %v", err)
	}

	// Verify health state-set.
	healthExpected := `
		# HELP zfs_pool_health 1 if pool is in the labeled state, 0 otherwise.
		# TYPE zfs_pool_health gauge
		zfs_pool_health{pool="tank",state="online"} 1
		zfs_pool_health{pool="tank",state="degraded"} 0
		zfs_pool_health{pool="tank",state="faulted"} 0
		zfs_pool_health{pool="tank",state="offline"} 0
		zfs_pool_health{pool="tank",state="removed"} 0
		zfs_pool_health{pool="tank",state="unavail"} 0
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(healthExpected), "zfs_pool_health"); err != nil {
		t.Errorf("health state-set mismatch: %v", err)
	}

	// Verify dataset share_nfs metric (labels alphabetized by prometheus).
	nfsExpected := `
		# HELP zfs_dataset_share_nfs 1 if NFS sharing is enabled, 0 otherwise.
		# TYPE zfs_dataset_share_nfs gauge
		zfs_dataset_share_nfs{dataset="tank",pool="tank",type="filesystem"} 0
		zfs_dataset_share_nfs{dataset="tank/media",pool="tank",type="filesystem"} 1
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(nfsExpected), "zfs_dataset_share_nfs"); err != nil {
		t.Errorf("NFS share mismatch: %v", err)
	}

	// Verify service_up metric.
	svcExpected := `
		# HELP zfs_service_up 1 if systemd unit is active, 0 otherwise.
		# TYPE zfs_service_up gauge
		zfs_service_up{service="nfs"} 1
		zfs_service_up{service="smb"} 1
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(svcExpected), "zfs_service_up"); err != nil {
		t.Errorf("service_up mismatch: %v", err)
	}

	// Verify scan metrics.
	scanExpected := `
		# HELP zfs_pool_scrub_active 1 if a scrub is in progress, 0 otherwise.
		# TYPE zfs_pool_scrub_active gauge
		zfs_pool_scrub_active{pool="tank"} 0
		# HELP zfs_pool_resilver_active 1 if a resilver (rebuild) is in progress, 0 otherwise.
		# TYPE zfs_pool_resilver_active gauge
		zfs_pool_resilver_active{pool="tank"} 0
		# HELP zfs_pool_scan_progress_ratio 0-1 progress of active scan, 0 if no scan active.
		# TYPE zfs_pool_scan_progress_ratio gauge
		zfs_pool_scan_progress_ratio{pool="tank"} 0
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(scanExpected),
		"zfs_pool_scrub_active", "zfs_pool_resilver_active", "zfs_pool_scan_progress_ratio"); err != nil {
		t.Errorf("scan metrics mismatch: %v", err)
	}
}

func TestCollector_PoolFailure_SetsUpZero(t *testing.T) {
	f := &fixtureRunner{
		poolErr: errors.New("command not found"),
	}

	coll := newTestCollector(f)

	expected := `
		# HELP zfs_up Whether ZFS commands succeeded.
		# TYPE zfs_up gauge
		zfs_up 0
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(expected), "zfs_up"); err != nil {
		t.Errorf("up metric mismatch on pool failure: %v", err)
	}

	// Should not emit any pool metrics.
	count := testutil.CollectAndCount(coll, "zfs_pool_size_bytes")
	if count != 0 {
		t.Errorf("expected 0 pool_size metrics on failure, got %d", count)
	}
}

func TestCollector_DatasetFailure_StillEmitsPools(t *testing.T) {
	f := &fixtureRunner{
		poolOut:    "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n",
		datasetErr: errors.New("zfs list failed"),
		statusOut: `  pool: tank
 state: ONLINE
  scan: none requested
`,
		svcResults: map[string]struct {
			output string
			err    error
		}{
			"nfs-kernel-server.service": {"active\n", nil},
			"smbd.service":              {"active\n", nil},
		},
	}

	coll := newTestCollector(f)

	// up should still be 1.
	upExpected := `
		# HELP zfs_up Whether ZFS commands succeeded.
		# TYPE zfs_up gauge
		zfs_up 1
	`

	if err := testutil.CollectAndCompare(coll, strings.NewReader(upExpected), "zfs_up"); err != nil {
		t.Errorf("up metric should be 1 when datasets fail: %v", err)
	}

	// Pool metrics should still be emitted.
	poolCount := testutil.CollectAndCount(coll, "zfs_pool_size_bytes")
	if poolCount != 1 {
		t.Errorf("expected 1 pool_size metric, got %d", poolCount)
	}

	// Dataset metrics should be absent.
	dsCount := testutil.CollectAndCount(coll, "zfs_dataset_used_bytes")
	if dsCount != 0 {
		t.Errorf("expected 0 dataset metrics on failure, got %d", dsCount)
	}
}

func TestCollector_DescriptorCount(t *testing.T) {
	f := &fixtureRunner{
		poolOut:    "tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n",
		datasetOut: "tank\t5368709120\t5368709120\t262144\tfilesystem\toff\toff\n",
		statusOut: `  pool: tank
 state: ONLINE
  scan: none requested
`,
		svcResults: map[string]struct {
			output string
			err    error
		}{
			"nfs-kernel-server.service": {"active\n", nil},
			"smbd.service":              {"active\n", nil},
		},
	}

	coll := newTestCollector(f)

	// 18 descriptors total: 2 meta + 7 pool + 3 scan + 5 dataset + 1 service
	descCount := 0
	ch := make(chan *prometheus.Desc, 50)
	coll.Describe(ch)
	close(ch)

	for range ch {
		descCount++
	}

	const expectedDescs = 18
	if descCount != expectedDescs {
		t.Errorf("expected %d descriptors, got %d", expectedDescs, descCount)
	}
}
