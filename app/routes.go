// Package app sets up HTTP routes and middleware for the application.
// It supports authentication, Swagger documentation (dev only), and monitoring endpoints.
// Routes:
// - Public routes without authentication (e.g., version)
// - Protected routes requiring API key or JWT
// - Swagger documentation (only in development) at /swagger/
// - Health, Live, Ready, Monitoring, and S0 data endpoints
//
// Middleware applied:
// - CORS
// - IP filtering (allowed/blocked IPs)
//
// This must be called during app startup before starting the HTTP server.
package app

import (
	"log/slog"
	"net/http"

	"github.com/womat/golib/web"
)

// SetupRoutes configures all HTTP routes and global middleware for the application.
func (app *App) SetupRoutes() {
	webCfg := web.Config{
		ApiKey:    app.config.Webserver.ApiKey,
		JwtSecret: app.config.Webserver.JwtSecret,
		JwtID:     app.config.Webserver.JwtID,
		AppName:   MODULE,
	}

	mux := http.NewServeMux()

	// Preflight CORS requests
	mux.Handle("OPTIONS /", web.HandlePreflight())

	// Dev-only Swagger documentation (only registered with -tags swagger)
	app.registerSwaggerRoute(mux)

	// Public routes
	mux.Handle("GET /version", app.HandleVersion())

	// Protected routes
	mux.Handle("GET /health", web.WithAuth(app.HandleHealth(), webCfg))

	// Apply global middleware: CORS + IP filter
	handler := web.WithCORS(mux)
	handler = web.WithIPFilter(handler, app.config.Webserver.AllowedIPs, app.config.Webserver.BlockedIPs)
	handler = WithLogging(handler)
	app.web.Handler = handler
}

func WithLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Incoming web request",
			"method", r.Method,
			"path", r.URL.Path,
			"client_ip", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
