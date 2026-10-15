package app

// HTTP routes and global middleware. The web page at /, /version and /ready are public;
// /health and /registers require a bearer token from the token file. Swagger UI is registered only in
// builds with the swagger tag. Middleware, outermost first: logging, IP filter, CORS.

import (
	"log/slog"
	"net/http"

	"github.com/womat/golib/web"
)

// SetupRoutes configures all HTTP routes and global middleware for the application.
func (app *App) SetupRoutes() {
	webCfg := app.webConfig()

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
	// One entry per request with client and caller. The web page polls every few seconds, so
	// successful requests go to Debug; a rejected one shows as a warning.
	handler = web.WithLogging(handler, slog.Default(), web.WithSuccessLevel(slog.LevelDebug))
	app.web.Handler = handler
}

// webConfig is the authentication of the protected routes: the bearer tokens from the token
// file, nothing else. With ApiKey, JwtSecret and JwtID left empty, golib's other paths stay
// disabled.
func (app *App) webConfig() web.Config {
	return web.Config{
		Tokens:  app.config.Webserver.Tokens,
		AppName: MODULE,
	}
}
