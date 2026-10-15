package app

// HTTP handler for /health: runtime metrics and the state of every meter, the RTU port and the
// Modbus listeners.

import (
	"net/http"
	"time"

	"github.com/womat/golib/web"
	"github.com/womat/smartmeter/app/service/health"
)

// HandleHealth returns the current health data of the application.
//
//	@Summary		Get health data
//	@Description	Retrieves memory usage, goroutine count, version, hostname, Go runtime version, OS, per meter the unit IDs, the source, the served values, the time, age and read duration of the last valid snapshot, whether the unit IDs answer, the last error, the snapshots discarded as implausible and the limits for the web page, the state of the RTU port and the Modbus listeners with their request counters.
//	@Tags			info
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	health.Model	"Health data successfully retrieved"
//	@Failure		401	{object}	web.ApiError	"Unauthorized"
//	@Router			/health [get]
func (app *App) HandleHealth() http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			resp := health.GetCurrentHealth(MODULE, VERSION)
			resp.Meters = app.modbusClient.Status(time.Now())
			resp.RTU = app.modbusServer.Status()
			resp.Modbus = app.modbusServer.Activity()
			web.Encode(w, http.StatusOK, resp)
		},
	)
}
