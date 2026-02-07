// Package exporter provides HTTP handlers for the ZFS exporter.
package exporter

import (
	"fmt"
	"log/slog"
	"net/http"
)

// LandingPageHandler returns an HTTP handler that serves a simple landing page
// with a link to the metrics endpoint.
func LandingPageHandler(metricsPath string, logger *slog.Logger) http.HandlerFunc {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>ZFS Exporter</title></head>
<body>
<h1>ZFS Exporter</h1>
<p><a href="%s">Metrics</a></p>
</body>
</html>`, metricsPath)

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if _, err := fmt.Fprint(w, html); err != nil {
			logger.Error("Failed to write landing page", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}
}
