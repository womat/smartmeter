package app

// HTTP routes and global middleware. The web page at /, /version and /ready are public;
// /health and /registers require the API key (X-API-Key). Swagger UI is registered only in
// builds with the swagger tag. Middleware, outermost first: logging, IP filter, CORS.

import (
	"log/slog"
	"net/http"

	"github.com/womat/golib/web"
)

// SetupRoutes configures all HTTP routes and global middleware for the application.
func (app *App) SetupRoutes() {
	// API key only: with JwtSecret and JwtID left empty, golib's JWT path stays disabled.
	webCfg := web.Config{
		ApiKey:  app.config.Webserver.ApiKey,
		AppName: MODULE,
	}

	mux := http.NewServeMux()

	// Preflight CORS requests
	mux.Handle("OPTIONS /", web.HandlePreflight())

	// Dev-only Swagger documentation (only registered with -tags swagger)
	app.registerSwaggerRoute(mux)

	// Public routes. {$} matches / only, so the page does not catch unknown paths.
	mux.Handle("GET /{$}", app.HandleUI())
	mux.Handle("GET /version", app.HandleVersion())
	mux.Handle("GET /ready", app.HandleReady())

	// Protected routes
	mux.Handle("GET /health", web.WithAuth(app.HandleHealth(), webCfg))
	mux.Handle("GET /registers", web.WithAuth(app.HandleRegisters(), webCfg))

	// Apply global middleware: CORS + IP filter. The API is read-only, so CORS advertises
	// GET and the preflight OPTIONS only.
	handler := web.WithCORS(mux, web.WithAllowedMethods(http.MethodGet, http.MethodOptions))
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
