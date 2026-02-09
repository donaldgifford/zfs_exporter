# Building a Prometheus Exporter in Go

This document captures the patterns and decisions used to build
`technitium_exporter`, a Prometheus exporter for Technitium DNS Server. Use this
as a reference when building new Prometheus exporters.

## Core Principles

1. Follow
   [Prometheus exporter best practices](https://prometheus.io/docs/instrumenting/writing_exporters/)
2. Follow the
   [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
3. Use `MustNewConstMetric` - never direct instrumentation
4. Test at the HTTP level with `httptest.Server` - no interface mocking
5. Keep the client in a public `pkg/` package, not `internal/`

---

## Project Structure

```
cmd/<exporter_name>/
    main.go                 # Entry point, flag parsing, HTTP server, graceful shutdown
collector/
    collector.go            # Prometheus collector (Describe + Collect)
    collector_test.go       # Tests using httptest.Server + testutil.CollectAndCompare
config/
    config.go               # Configuration (kingpin flags + env var overrides)
    errors.go               # Sentinel errors for validation
exporter/
    exporter.go             # HTTP handlers (landing page)
pkg/<service_name>/
    client.go               # API client (HTTP, context, error wrapping)
    client_test.go           # Client tests using httptest.Server
    types.go                # API response structs
deploy/
    deb/                    # Debian package files
        systemd/            # Systemd service unit
        default/            # Environment config template
        scripts/            # postinstall/preremove scripts
        copyright           # Debian copyright
        lintian-overrides   # Suppress expected Go binary warnings
contrib/
    grafana/                # Grafana dashboard JSON
    prometheus/             # Alert rules
```

---

## Collector Pattern

The collector is the core of a Prometheus exporter. It implements
`prometheus.Collector` with two methods: `Describe` and `Collect`.

### Key Rules

- Define metric descriptors once in the constructor using `prometheus.NewDesc`
- Use `prometheus.BuildFQName(namespace, subsystem, name)` for consistent naming
- In `Collect()`, create fresh metrics each scrape with `MustNewConstMetric`
- Never store metric state between scrapes

### Struct and Constructor

```go
const namespace = "myservice"

type Collector struct {
    client *myservice.Client
    logger *slog.Logger

    // Metric descriptors (immutable, created once)
    up             *prometheus.Desc
    scrapeDuration *prometheus.Desc
    queriesTotal   *prometheus.Desc
    // ...
}

func NewCollector(client *myservice.Client, logger *slog.Logger) *Collector {
    return &Collector{
        client: client,
        logger: logger,
        up: prometheus.NewDesc(
            prometheus.BuildFQName(namespace, "", "up"),
            "Whether the server is reachable.",
            nil, nil,
        ),
        scrapeDuration: prometheus.NewDesc(
            prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
            "Time taken to scrape metrics.",
            nil, nil,
        ),
        queriesTotal: prometheus.NewDesc(
            prometheus.BuildFQName(namespace, "", "queries_total"),
            "Total queries processed.",
            nil, nil,
        ),
    }
}
```

### Describe Method

Send all metric descriptors. Called once at registration.

```go
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
    ch <- c.up
    ch <- c.scrapeDuration
    ch <- c.queriesTotal
}
```

### Collect Method

Fetch data and emit fresh metrics. Called on every scrape.

```go
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
    start := time.Now()
    ctx := context.Background()

    // Concurrent API calls for independent endpoints
    var wg sync.WaitGroup
    var stats *myservice.StatsResponse
    var settings *myservice.SettingsResponse
    var statsErr, settingsErr error

    wg.Add(2)
    go func() {
        defer wg.Done()
        stats, statsErr = c.client.GetStats(ctx)
    }()
    go func() {
        defer wg.Done()
        settings, settingsErr = c.client.GetSettings(ctx)
    }()
    wg.Wait()

    duration := time.Since(start).Seconds()
    ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, duration)

    // Primary endpoint failure = exporter is down
    if statsErr != nil {
        c.logger.Error("Failed to get stats", "err", statsErr)
        ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
        return
    }

    ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

    // Optional endpoint failure = graceful fallback
    if settingsErr != nil {
        c.logger.Warn("Failed to get settings", "err", settingsErr)
        // Use fallback values instead of failing
    }

    // Emit metrics
    ch <- prometheus.MustNewConstMetric(c.queriesTotal, prometheus.CounterValue, float64(stats.TotalQueries))

    // Labeled metrics: same descriptor, different label values
    ch <- prometheus.MustNewConstMetric(c.responsesTotal, prometheus.CounterValue, float64(stats.NoError), "noerror")
    ch <- prometheus.MustNewConstMetric(c.responsesTotal, prometheus.CounterValue, float64(stats.ServFail), "servfail")
}
```

### Error Handling Strategy

- **Required endpoints**: If they fail, set `up=0` and return early
- **Optional endpoints**: Log a warning, use fallback values, continue emitting
  metrics
- Always emit `up` and `scrape_duration_seconds` regardless of errors

---

## API Client Pattern

The client lives in `pkg/<service>/` as a public package. No interfaces needed -
tests mock at the HTTP level.

```go
type Client struct {
    baseURL    string
    token      string
    httpClient *http.Client
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
    return &Client{
        baseURL: baseURL,
        token:   token,
        httpClient: &http.Client{
            Timeout: timeout,
        },
    }
}
```

### Request Pattern

```go
func (c *Client) doRequest(ctx context.Context, endpoint string, params url.Values) (*http.Response, error) {
    reqURL := c.baseURL + endpoint + "?" + params.Encode()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }

    if resp.StatusCode != http.StatusOK {
        _ = resp.Body.Close()
        return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }

    return resp, nil
}
```

### Endpoint Methods

```go
func (c *Client) GetStats(ctx context.Context) (*StatsResponse, error) {
    params := url.Values{}
    params.Set("token", c.token)

    resp, err := c.doRequest(ctx, "/api/stats", params)
    if err != nil {
        return nil, fmt.Errorf("failed to get stats: %w", err)
    }
    defer func() {
        _ = resp.Body.Close()
    }()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response body: %w", err)
    }

    var result StatsResponse
    if err := json.Unmarshal(body, &result); err != nil {
        return nil, fmt.Errorf("failed to parse response: %w", err)
    }

    if result.Status != "ok" {
        return nil, fmt.Errorf("API returned non-ok status: %s", result.Status)
    }

    return &result, nil
}
```

### Key Rules

- Context as first parameter, always
- Wrap all errors with `%w` and descriptive context
- Close response bodies on error paths
- Validate both HTTP status and API-level status
- Deferred close on success paths

---

## Testing Pattern

Tests use `net/http/httptest` for HTTP-level mocking. No mockery, no interface
mocking. This follows Prometheus exporter conventions and tests the real client
code path.

### Test Server Factory

Create a reusable helper that routes to the right endpoint:

```go
func newTestServer(statsJSON, settingsJSON string, statsCode int) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch {
        case strings.Contains(r.URL.Path, "/api/stats"):
            w.WriteHeader(statsCode)
            _, _ = w.Write([]byte(statsJSON))
        case strings.Contains(r.URL.Path, "/api/settings"):
            _, _ = w.Write([]byte(settingsJSON))
        default:
            w.WriteHeader(http.StatusNotFound)
        }
    }))
}
```

### Test Data Helpers

Base test fixtures on real API responses:

```go
func realWorldStatsJSON() string {
    return `{
        "status": "ok",
        "response": {
            "stats": {
                "totalQueries": 72,
                "totalNoError": 72
            }
        }
    }`
}
```

### Collector Tests with testutil.CollectAndCompare

Use the Prometheus test utility to validate metric output format:

```go
func TestCollector_RealWorldData(t *testing.T) {
    server := newTestServer(realWorldStatsJSON(), settingsJSON(), http.StatusOK)
    defer server.Close()

    client := myservice.NewClient(server.URL, "test-token", 5*time.Second)
    coll := NewCollector(client, newTestLogger())

    expected := `
        # HELP myservice_queries_total Total queries processed.
        # TYPE myservice_queries_total counter
        myservice_queries_total 72
    `
    if err := testutil.CollectAndCompare(coll, strings.NewReader(expected), "myservice_queries_total"); err != nil {
        t.Errorf("unexpected metric: %v", err)
    }
}
```

### Client Tests (Table-Driven)

```go
func TestClient_GetStats(t *testing.T) {
    tests := []struct {
        name       string
        body       string
        status     int
        wantErr    bool
    }{
        {"success", `{"status":"ok","response":{...}}`, 200, false},
        {"server error", "", 500, true},
        {"api error", `{"status":"error"}`, 200, true},
        {"invalid json", `not json`, 200, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(tt.status)
                _, _ = w.Write([]byte(tt.body))
            }))
            defer server.Close()

            client := NewClient(server.URL, "test-token", 10*time.Second)
            _, err := client.GetStats(context.Background())
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### What to Test

- **Happy path**: Real-world data, metric values, labels
- **Partial failure**: Optional endpoint fails, exporter still works
- **Total failure**: All endpoints fail, `up=0`
- **HTTP errors**: 500s, connection refused, timeouts
- **Metric counts**: Correct number of metrics emitted
- **Descriptors**: Correct number of Describe() descriptors

---

## Configuration Pattern

Use kingpin for CLI flags with environment variable overrides.

```go
type Config struct {
    ServiceURL   string
    ServiceToken string
    ListenAddress string
    MetricsPath   string
    ScrapeTimeout time.Duration
}

func NewConfig(app *kingpin.Application) *Config {
    cfg := &Config{}
    app.Flag("service.url", "URL of the service API.").
        Default("").StringVar(&cfg.ServiceURL)
    app.Flag("web.listen-address", "Address to listen on.").
        Default(":9167").StringVar(&cfg.ListenAddress)
    app.Flag("scrape.timeout", "Timeout for API calls.").
        Default("10s").DurationVar(&cfg.ScrapeTimeout)
    return cfg
}

// Environment variables override flags
func (c *Config) ApplyEnvironment() {
    if v := os.Getenv("SERVICE_URL"); v != "" {
        c.ServiceURL = v
    }
}

// Validate required fields
func (c *Config) Validate() error {
    if c.ServiceURL == "" {
        return ErrMissingURL
    }
    return nil
}
```

**Precedence**: defaults -> flags -> environment variables.

---

## Main Entry Point Pattern

```go
func main() {
    // 1. Parse flags
    app := kingpin.New("myservice_exporter", "Prometheus exporter for MyService.")
    cfg := config.NewConfig(app)
    logLevel := app.Flag("log.level", "Log level.").Default("info").Enum("debug", "info", "warn", "error")
    kingpin.MustParse(app.Parse(os.Args[1:]))

    // 2. Setup structured logging (stdlib slog, not go-kit/log)
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(*logLevel)}))

    // 3. Apply env vars and validate
    cfg.ApplyEnvironment()
    if err := cfg.Validate(); err != nil {
        logger.Error("Configuration error", "err", err)
        os.Exit(1)
    }

    // 4. Create client and register collector
    client := myservice.NewClient(cfg.ServiceURL, cfg.ServiceToken, cfg.ScrapeTimeout)
    coll := collector.NewCollector(client, logger)
    prometheus.MustRegister(coll)

    // 5. HTTP server with timeouts
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

    // 6. Graceful shutdown
    go func() {
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        <-sigCh
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        _ = server.Shutdown(ctx)
    }()

    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Error("Server error", "err", err)
        os.Exit(1)
    }
}
```

### Key Decisions

- **slog** over go-kit/log: stdlib, no external dependency, structured by
  default
- **Server timeouts**: Required to prevent slow-client attacks
- **Graceful shutdown**: Handles SIGINT/SIGTERM, drains in-flight requests
- **http.ErrServerClosed**: Expected after shutdown, not an error

---

## Debian Packaging with goreleaser + nfpms

### goreleaser nfpms config

```yaml
nfpms:
  - package_name: myservice-exporter
    description: |-
      Prometheus exporter for MyService.
      Multi-line extended description here.
    license: Apache-2.0
    formats:
      - deb
    bindir: /usr/bin
    section: net
    changelog: changelog.yml
    contents:
      - src: deploy/deb/systemd/myservice_exporter.service
        dst: /lib/systemd/system/myservice_exporter.service
      - src: deploy/deb/default/myservice_exporter
        dst: /etc/default/myservice_exporter
        type: config|noreplace
      - src: deploy/deb/copyright
        dst: /usr/share/doc/myservice-exporter/copyright
      - src: deploy/deb/lintian-overrides
        dst: /usr/share/lintian/overrides/myservice-exporter
        file_info:
          mode: 0644
    scripts:
      postinstall: deploy/deb/scripts/postinstall.sh
      preremove: deploy/deb/scripts/preremove.sh
    overrides:
      deb:
        dependencies:
          - systemd
```

### Systemd Service

Key security hardening for the service unit:

```ini
[Service]
Type=simple
User=myservice_exporter
Group=myservice_exporter
EnvironmentFile=-/etc/default/myservice_exporter
ExecStart=/usr/bin/myservice_exporter $MYSERVICE_EXPORTER_OPTS
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
```

### Postinstall Script

Create system user, enable service, print setup instructions:

```sh
#!/bin/sh
set -e
if ! getent group myservice_exporter >/dev/null 2>&1; then
    groupadd --system myservice_exporter
fi
if ! getent passwd myservice_exporter >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin \
        --gid myservice_exporter myservice_exporter
fi
systemctl daemon-reload
systemctl enable myservice_exporter.service
```

Do not start the service automatically - user must configure credentials first.

### Changelog with chglog

Use [chglog](https://github.com/goreleaser/chglog) for Debian-compatible
changelogs:

```yaml
# .chglog.yml
conventional-commits: true
exclude-merge-commits: true
package-name: "myservice-exporter"
deb:
  distribution:
    - stable
  urgency: low
```

```yaml
# changelog.yml
- semver: 0.1.0
  date: 2025-02-02T12:00:00Z
  packager: Your Name <email@example.com>
  deb:
    distributions:
      - stable
    urgency: low
  changes:
    - commit: abc123
      note: "feat: initial release"
```

### Lintian Overrides

Suppress expected warnings for Go static binaries:

```
myservice-exporter: statically-linked-binary [usr/bin/myservice_exporter]
```

### CI Lintian Validation

Add a package-lint job that builds the deb and runs lintian:

```yaml
package-lint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v5
      with:
        fetch-depth: 0
    - uses: actions/setup-go@v6
      with:
        go-version-file: go.mod
    - uses: goreleaser/goreleaser-action@v6
      with:
        args: release --snapshot --clean --skip=publish,sign
    - run: sudo apt-get update && sudo apt-get install -y lintian
    - run: lintian --no-cfg dist/myservice-exporter_*_amd64.deb
```

---

## Metric Naming Conventions

- Prefix all metrics with `<namespace>_`
- Use `snake_case`
- Suffix counters with `_total`
- Use base units: `_seconds` not `_milliseconds`, `_bytes` not `_kilobytes`
- Never include label names in metric names
- Use `BuildFQName(namespace, subsystem, name)` for consistency

---

## Checklist

Before shipping a Prometheus exporter:

- [ ] All metrics use `MustNewConstMetric` (no direct instrumentation)
- [ ] `<namespace>_up` gauge indicates server reachability
- [ ] `<namespace>_scrape_duration_seconds` gauge tracks scrape time
- [ ] Tests use `httptest.Server`, not interface mocks
- [ ] Tests use `testutil.CollectAndCompare` for metric validation
- [ ] `golangci-lint` passes with strict config
- [ ] `go test -race ./...` passes
- [ ] HTTP server has all four timeouts set
- [ ] Graceful shutdown handles SIGINT and SIGTERM
- [ ] Config supports both CLI flags and environment variables
- [ ] Errors are wrapped with `%w` and descriptive context
- [ ] Response bodies are closed on all paths
- [ ] Debian package passes lintian
- [ ] Systemd service runs as non-root with security hardening
