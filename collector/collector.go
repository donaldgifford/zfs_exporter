// Package collector implements the Prometheus collector for ZFS metrics.
package collector

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/donaldgifford/zfs_exporter/pkg/host"
	"github.com/donaldgifford/zfs_exporter/pkg/zfs"
)

const namespace = "zfs"

// healthStates enumerates all possible pool health states.
var healthStates = []string{"online", "degraded", "faulted", "offline", "removed", "unavail"}

// Collector collects ZFS metrics.
type Collector struct {
	client     *zfs.Client
	svcChecker *host.ServiceChecker
	logger     *slog.Logger
	timeout    time.Duration
	services   map[string][]string

	// Meta
	up             *prometheus.Desc
	scrapeDuration *prometheus.Desc

	// Pool
	poolSize          *prometheus.Desc
	poolAllocated     *prometheus.Desc
	poolFree          *prometheus.Desc
	poolFragmentation *prometheus.Desc
	poolDedup         *prometheus.Desc
	poolReadOnly      *prometheus.Desc
	poolHealth        *prometheus.Desc

	// Pool scan
	poolScrubActive    *prometheus.Desc
	poolResilverActive *prometheus.Desc
	poolScanProgress   *prometheus.Desc

	// Dataset
	datasetUsed       *prometheus.Desc
	datasetAvailable  *prometheus.Desc
	datasetReferenced *prometheus.Desc
	datasetShareNFS   *prometheus.Desc
	datasetShareSMB   *prometheus.Desc

	// Service
	serviceUp *prometheus.Desc
}

// NewCollector creates a new Collector.
func NewCollector(
	client *zfs.Client,
	svcChecker *host.ServiceChecker,
	logger *slog.Logger,
	timeout time.Duration,
	services map[string][]string,
) *Collector {
	c := &Collector{
		client:     client,
		svcChecker: svcChecker,
		logger:     logger,
		timeout:    timeout,
		services:   services,
	}
	c.initDescriptors()

	return c
}

func (c *Collector) initDescriptors() {
	poolLabels := []string{"pool"}
	datasetLabels := []string{"dataset", "type", "pool"}

	// Meta.
	c.up = prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "up"), "Whether ZFS commands succeeded.", nil, nil)
	c.scrapeDuration = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
		"Time taken to collect all metrics.",
		nil,
		nil,
	)

	// Pool.
	c.poolSize = prometheus.NewDesc(prometheus.BuildFQName(namespace, "pool", "size_bytes"), "Total pool size in bytes.", poolLabels, nil)
	c.poolAllocated = prometheus.NewDesc(prometheus.BuildFQName(namespace, "pool", "allocated_bytes"), "Allocated space in bytes.", poolLabels, nil)
	c.poolFree = prometheus.NewDesc(prometheus.BuildFQName(namespace, "pool", "free_bytes"), "Free space in bytes.", poolLabels, nil)
	c.poolFragmentation = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "fragmentation_ratio"),
		"Pool fragmentation as a ratio (0-1), NaN if unavailable.",
		poolLabels,
		nil,
	)
	c.poolDedup = prometheus.NewDesc(prometheus.BuildFQName(namespace, "pool", "dedup_ratio"), "Deduplication ratio.", poolLabels, nil)
	c.poolReadOnly = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "readonly"),
		"1 if pool is read-only, 0 otherwise.",
		poolLabels,
		nil,
	)
	c.poolHealth = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "health"),
		"1 if pool is in the labeled state, 0 otherwise.",
		[]string{"pool", "state"},
		nil,
	)

	// Scan.
	c.poolScrubActive = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "scrub_active"),
		"1 if a scrub is in progress, 0 otherwise.",
		poolLabels,
		nil,
	)
	c.poolResilverActive = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "resilver_active"),
		"1 if a resilver (rebuild) is in progress, 0 otherwise.",
		poolLabels,
		nil,
	)
	c.poolScanProgress = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "pool", "scan_progress_ratio"),
		"0-1 progress of active scan, 0 if no scan active.",
		poolLabels,
		nil,
	)

	// Dataset.
	c.datasetUsed = prometheus.NewDesc(prometheus.BuildFQName(namespace, "dataset", "used_bytes"), "Space consumed by dataset.", datasetLabels, nil)
	c.datasetAvailable = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "dataset", "available_bytes"),
		"Space available to dataset.",
		datasetLabels,
		nil,
	)
	c.datasetReferenced = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "dataset", "referenced_bytes"),
		"Space referenced by dataset.",
		datasetLabels,
		nil,
	)
	c.datasetShareNFS = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "dataset", "share_nfs"),
		"1 if NFS sharing is enabled, 0 otherwise.",
		datasetLabels,
		nil,
	)
	c.datasetShareSMB = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "dataset", "share_smb"),
		"1 if SMB sharing is enabled, 0 otherwise.",
		datasetLabels,
		nil,
	)

	// Service.
	c.serviceUp = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "service_up"),
		"1 if systemd unit is active, 0 otherwise.",
		[]string{"service"},
		nil,
	)
}

// Describe sends all metric descriptors.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.scrapeDuration
	ch <- c.poolSize
	ch <- c.poolAllocated
	ch <- c.poolFree
	ch <- c.poolFragmentation
	ch <- c.poolDedup
	ch <- c.poolReadOnly
	ch <- c.poolHealth
	ch <- c.poolScrubActive
	ch <- c.poolResilverActive
	ch <- c.poolScanProgress
	ch <- c.datasetUsed
	ch <- c.datasetAvailable
	ch <- c.datasetReferenced
	ch <- c.datasetShareNFS
	ch <- c.datasetShareSMB
	ch <- c.serviceUp
}

// Collect fetches ZFS data and emits metrics.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// Fetch pools (required).
	pools, poolErr := c.client.GetPools(ctx)

	duration := time.Since(start).Seconds()
	ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, duration)

	if poolErr != nil {
		c.logger.Error("Failed to get pools", "err", poolErr)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)

		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	// Emit pool metrics.
	c.collectPoolMetrics(ch, pools)

	// Fetch optional data concurrently.
	var (
		datasets []zfs.Dataset
		scans    []zfs.ScanStatus
		svcs     []host.ServiceStatus
		dsErr    error
		scanErr  error
		svcErr   error
		wg       sync.WaitGroup
	)

	wg.Add(3) //nolint:mnd // three concurrent fetches

	go func() {
		defer wg.Done()
		datasets, dsErr = c.client.GetDatasets(ctx)
	}()

	go func() {
		defer wg.Done()
		scans, scanErr = c.client.GetScanStatuses(ctx)
	}()

	go func() {
		defer wg.Done()
		svcs, svcErr = c.svcChecker.CheckServices(ctx, c.services)
	}()

	wg.Wait()

	// Dataset metrics (optional).
	if dsErr != nil {
		c.logger.Warn("Failed to get datasets", "err", dsErr)
	} else {
		c.collectDatasetMetrics(ch, datasets)
	}

	// Scan metrics (optional).
	if scanErr != nil {
		c.logger.Warn("Failed to get scan statuses", "err", scanErr)
	} else {
		c.collectScanMetrics(ch, scans)
	}

	// Service metrics (optional).
	if svcErr != nil {
		c.logger.Warn("Failed to check services", "err", svcErr)
	} else {
		c.collectServiceMetrics(ch, svcs)
	}
}

func (c *Collector) collectPoolMetrics(ch chan<- prometheus.Metric, pools []zfs.Pool) {
	for _, p := range pools {
		ch <- prometheus.MustNewConstMetric(c.poolSize, prometheus.GaugeValue, float64(p.Size), p.Name)
		ch <- prometheus.MustNewConstMetric(c.poolAllocated, prometheus.GaugeValue, float64(p.Allocated), p.Name)
		ch <- prometheus.MustNewConstMetric(c.poolFree, prometheus.GaugeValue, float64(p.Free), p.Name)
		ch <- prometheus.MustNewConstMetric(c.poolFragmentation, prometheus.GaugeValue, p.Fragmentation, p.Name)
		ch <- prometheus.MustNewConstMetric(c.poolDedup, prometheus.GaugeValue, p.DedupRatio, p.Name)

		ro := 0.0
		if p.ReadOnly {
			ro = 1.0
		}

		ch <- prometheus.MustNewConstMetric(c.poolReadOnly, prometheus.GaugeValue, ro, p.Name)

		// Health state-set: one metric per possible state.
		healthLower := strings.ToLower(p.Health)
		for _, state := range healthStates {
			val := 0.0
			if state == healthLower {
				val = 1.0
			}

			ch <- prometheus.MustNewConstMetric(c.poolHealth, prometheus.GaugeValue, val, p.Name, state)
		}
	}
}

func (c *Collector) collectScanMetrics(ch chan<- prometheus.Metric, scans []zfs.ScanStatus) {
	for _, s := range scans {
		scrub := 0.0
		if s.Scrub {
			scrub = 1.0
		}

		resilver := 0.0
		if s.Resilver {
			resilver = 1.0
		}

		ch <- prometheus.MustNewConstMetric(c.poolScrubActive, prometheus.GaugeValue, scrub, s.Pool)
		ch <- prometheus.MustNewConstMetric(c.poolResilverActive, prometheus.GaugeValue, resilver, s.Pool)
		ch <- prometheus.MustNewConstMetric(c.poolScanProgress, prometheus.GaugeValue, s.Progress, s.Pool)
	}
}

func (c *Collector) collectDatasetMetrics(ch chan<- prometheus.Metric, datasets []zfs.Dataset) {
	for _, d := range datasets {
		ch <- prometheus.MustNewConstMetric(c.datasetUsed, prometheus.GaugeValue, float64(d.Used), d.Name, d.Type, d.Pool)
		ch <- prometheus.MustNewConstMetric(c.datasetAvailable, prometheus.GaugeValue, float64(d.Available), d.Name, d.Type, d.Pool)
		ch <- prometheus.MustNewConstMetric(c.datasetReferenced, prometheus.GaugeValue, float64(d.Referenced), d.Name, d.Type, d.Pool)

		nfs := 0.0
		if d.ShareNFS {
			nfs = 1.0
		}

		smb := 0.0
		if d.ShareSMB {
			smb = 1.0
		}

		ch <- prometheus.MustNewConstMetric(c.datasetShareNFS, prometheus.GaugeValue, nfs, d.Name, d.Type, d.Pool)
		ch <- prometheus.MustNewConstMetric(c.datasetShareSMB, prometheus.GaugeValue, smb, d.Name, d.Type, d.Pool)
	}
}

func (c *Collector) collectServiceMetrics(ch chan<- prometheus.Metric, svcs []host.ServiceStatus) {
	for _, s := range svcs {
		val := 0.0
		if s.Active {
			val = 1.0
		}

		ch <- prometheus.MustNewConstMetric(c.serviceUp, prometheus.GaugeValue, val, s.Name)
	}
}
