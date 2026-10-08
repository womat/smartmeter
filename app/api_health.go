package app

// HTTP handlers for health and readiness: /health returns runtime metrics and the state of
// every meter, /ready is a readiness probe for monitoring.

import (
	"errors"
	"net/http"
	"time"

	"github.com/womat/golib/web"
	"github.com/womat/smartmeter/app/service/health"
)

// HandleHealth returns the current health data of the application.
//
//	@Summary		Get health data
//	@Description	Retrieves memory usage, goroutine count, version, hostname, Go runtime version, OS, per meter the unit IDs, the time and age of the last valid snapshot, the last error and the snapshots discarded as implausible, and the state of the RTU port.
//	@Tags			info
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Success		200	{object}	health.Model	"Health data successfully retrieved"
//	@Failure		401	{object}	web.ApiError	"Unauthorized"
//	@Router			/health [get]
func (app *App) HandleHealth() http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			resp := health.GetCurrentHealth(MODULE, VERSION)
			resp.Meters = app.modbusClient.Status(time.Now())
			resp.RTU = app.modbusServer.Status()
			web.Encode(w, http.StatusOK, resp)
		},
	)
}

// HandleReady is a readiness probe for monitoring.
// It returns 200 OK while every meter serves current values, and 503 Service Unavailable
// while a meter has delivered no valid snapshot for three poll intervals, because the
// inverter and the other clients then read outdated values, or while the RTU port is not
// available, because the inverter then gets no values at all.
//
//	@Summary		Readiness check
//	@Description	Returns 200 while every meter serves current values and the RTU port is available, 503 while a meter has delivered no valid snapshot for three poll intervals or the RTU port is not available. No authentication required.
//	@Tags			info
//	@Produce		json
//	@Success		200	{object}	map[string]string	"Application is ready"
//	@Failure		503	{object}	web.ApiError		"Values of a meter are outdated"
//	@Router			/ready [get]
func (app *App) HandleReady() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := errors.Join(app.modbusClient.Ready(time.Now()), app.modbusServer.Ready())
		if err != nil {
			// Not WriteError: from 500 on it replaces the message with "internal server error",
			// but a readiness probe should say why the service is not ready.
			web.Encode(w, http.StatusServiceUnavailable, web.NewApiError(err))
			return
		}

		web.Encode(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}
