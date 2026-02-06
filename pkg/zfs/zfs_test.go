package zfs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, nil))
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestClient_GetPools_Success(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("tank\t10737418240\t5368709120\t5368709120\t33\t1.00\tONLINE\toff\n"), nil
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	pools, err := client.GetPools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}

	if pools[0].Name != "tank" {
		t.Errorf("pool name = %q, want %q", pools[0].Name, "tank")
	}
}

func TestClient_GetPools_CommandError(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("command not found")
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	_, err := client.GetPools(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_GetPools_EmptyOutput(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(""), nil
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	pools, err := client.GetPools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pools) != 0 {
		t.Fatalf("expected 0 pools, got %d", len(pools))
	}
}

func TestClient_GetDatasets_Success(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("tank/media\t4294967296\t5368709120\t4294967296\tfilesystem\ton\toff\n"), nil
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	datasets, err := client.GetDatasets(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(datasets) != 1 {
		t.Fatalf("expected 1 dataset, got %d", len(datasets))
	}

	if datasets[0].Name != "tank/media" {
		t.Errorf("dataset name = %q, want %q", datasets[0].Name, "tank/media")
	}

	if !datasets[0].ShareNFS {
		t.Error("expected ShareNFS = true")
	}
}

func TestClient_GetDatasets_CommandError(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("command failed")
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	_, err := client.GetDatasets(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_GetScanStatuses_Success(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`  pool: tank
 state: ONLINE
  scan: scrub in progress since Sun Jul 25 16:07:49 2025
    374G scanned at 161M/s, 340G issued at 146M/s, 703G total
    0B repaired, 48.36% done, 00:42:27 to go
`), nil
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	statuses, err := client.GetScanStatuses(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	if !statuses[0].Scrub {
		t.Error("expected Scrub = true")
	}
}

func TestClient_GetScanStatuses_CommandError(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("command failed")
	}

	client := NewClient(runner, testLogger(), "zpool", "zfs")

	_, err := client.GetScanStatuses(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestClient_VerifiesBinaryPaths(t *testing.T) {
	var capturedName string

	runner := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		capturedName = name

		return []byte(""), nil
	}

	client := NewClient(runner, testLogger(), "/usr/sbin/zpool", "/usr/sbin/zfs")

	_, _ = client.GetPools(context.Background())

	if capturedName != "/usr/sbin/zpool" {
		t.Errorf("expected runner called with %q, got %q", "/usr/sbin/zpool", capturedName)
	}

	_, _ = client.GetDatasets(context.Background())

	if capturedName != "/usr/sbin/zfs" {
		t.Errorf("expected runner called with %q, got %q", "/usr/sbin/zfs", capturedName)
	}
}
