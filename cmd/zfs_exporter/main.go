// Package main is the entry point for the zfs_exporter binary.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/donaldgifford/zfs_exporter/collector"
	"github.com/donaldgifford/zfs_exporter/config"
	"github.com/donaldgifford/zfs_exporter/exporter"
	"github.com/donaldgifford/zfs_exporter/pkg/host"
	"github.com/donaldgifford/zfs_exporter/pkg/zfs"
)

// Version information set by ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	app := kingpin.New("zfs_exporter", "Prometheus exporter for ZFS.")
	app.Version(fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, BuildDate))
	app.HelpFlag.Short('h')

	cfg := config.NewConfig(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))

	logger := setupLogger(cfg.LogLevel)

	cfg.ApplyEnvironment()

	if err := cfg.Validate(); err != nil {
		logger.Error("Configuration validation failed", "err", err)
		os.Exit(1)
	}

	logger.Info("Starting zfs_exporter",
		"version", Version,
		"listen", cfg.ListenAddress,
		"zpool_path", cfg.ZpoolPath,
		"zfs_path", cfg.ZfsPath,
		"services", cfg.Services,
	)

	// Create ZFS client and service checker.
	runner := zfs.DefaultRunner()
	client := zfs.NewClient(runner, logger, cfg.ZpoolPath, cfg.ZfsPath)
	svcChecker := host.NewServiceChecker(runner, logger)

	// Build service map from configured keys.
	services := buildServiceMap(cfg.Services)

	// Register collector.
	coll := collector.NewCollector(client, svcChecker, logger, cfg.ScrapeTimeout, services)
	prometheus.MustRegister(coll)

	// HTTP server.
	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.Handler())
	mux.HandleFunc("/", exporter.LandingPageHandler(cfg.MetricsPath))

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("Received signal, shutting down", "signal", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("HTTP server shutdown error", "err", err)
		}
	}()

	logger.Info("Listening", "address", cfg.ListenAddress)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("HTTP server error", "err", err)
		os.Exit(1)
	}

	logger.Info("Exporter stopped")
}

func setupLogger(level string) *slog.Logger {
	var lvl slog.Level

	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

// buildServiceMap maps configured service keys to their candidate systemd unit names.
func buildServiceMap(keys []string) map[string][]string {
	result := make(map[string][]string, len(keys))

	for _, key := range keys {
		if units, ok := host.DefaultServiceUnits[key]; ok {
			result[key] = units
		}
	}

	return result
}
