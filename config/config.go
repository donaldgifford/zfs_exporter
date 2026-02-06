// Package config handles CLI flags, environment variable overrides, and validation.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
)

// Config holds all exporter configuration.
type Config struct {
	ListenAddress string
	MetricsPath   string
	LogLevel      string
	ScrapeTimeout time.Duration
	ZpoolPath     string
	ZfsPath       string
	Services      []string
	servicesRaw   string
}

// NewConfig registers flags on the given kingpin application and returns a Config.
func NewConfig(app *kingpin.Application) *Config {
	cfg := &Config{}

	app.Flag("web.listen-address", "Address to listen on for HTTP requests.").
		Default(":9134").StringVar(&cfg.ListenAddress)
	app.Flag("web.metrics-path", "Path under which to expose metrics.").
		Default("/metrics").StringVar(&cfg.MetricsPath)
	app.Flag("log.level", "Log level.").
		Default("info").EnumVar(&cfg.LogLevel, "debug", "info", "warn", "error")
	app.Flag("scrape.timeout", "Total timeout budget for all commands in a single scrape.").
		Default("10s").DurationVar(&cfg.ScrapeTimeout)
	app.Flag("zfs.zpool-path", "Path to the zpool binary.").
		Default("zpool").StringVar(&cfg.ZpoolPath)
	app.Flag("zfs.zfs-path", "Path to the zfs binary.").
		Default("zfs").StringVar(&cfg.ZfsPath)
	app.Flag("host.services", "Comma-separated list of service keys to monitor.").
		Default("zfs,nfs,smb,iscsi").StringVar(&cfg.servicesRaw)

	return cfg
}

// Validate checks that required binaries exist and parses the service list.
func (c *Config) Validate() error {
	c.parseServices()

	if err := c.validateBinary(c.ZpoolPath, ErrZpoolNotFound); err != nil {
		return err
	}

	if err := c.validateBinary(c.ZfsPath, ErrZfsNotFound); err != nil {
		return err
	}

	return nil
}

// ApplyEnvironment applies environment variable overrides.
func (c *Config) ApplyEnvironment() {
	if v := os.Getenv("ZFS_EXPORTER_LISTEN_ADDRESS"); v != "" {
		c.ListenAddress = v
	}

	if v := os.Getenv("ZFS_EXPORTER_METRICS_PATH"); v != "" {
		c.MetricsPath = v
	}

	if v := os.Getenv("ZFS_EXPORTER_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}

	if v := os.Getenv("ZFS_EXPORTER_SCRAPE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.ScrapeTimeout = d
		}
	}

	if v := os.Getenv("ZFS_EXPORTER_ZPOOL_PATH"); v != "" {
		c.ZpoolPath = v
	}

	if v := os.Getenv("ZFS_EXPORTER_ZFS_PATH"); v != "" {
		c.ZfsPath = v
	}

	if v := os.Getenv("ZFS_EXPORTER_SERVICES"); v != "" {
		c.servicesRaw = v
	}
}

func (c *Config) parseServices() {
	if c.servicesRaw == "" {
		c.Services = nil
		return
	}

	parts := strings.Split(c.servicesRaw, ",")
	c.Services = make([]string, 0, len(parts))

	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			c.Services = append(c.Services, s)
		}
	}
}

func (*Config) validateBinary(path string, sentinel error) error {
	// If the path is a bare name (no /), use LookPath.
	if !strings.Contains(path, "/") {
		_, err := exec.LookPath(path)
		if err != nil {
			return fmt.Errorf("%w: %s", sentinel, path)
		}

		return nil
	}

	// Absolute or relative path — check directly.
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: %s", sentinel, path)
	}

	// Check executable bit.
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%w: %s (not executable)", sentinel, path)
	}

	return nil
}
