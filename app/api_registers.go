package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/womat/golib/web"
	"github.com/womat/smartmeter/app/service/meters"
)

// HandleRegisters returns both register maps of one unit ID as a client reads them.
//
//	@Summary		Get the registers of a unit ID
//	@Description	Returns the proprietary Fronius map (read by the inverter) and the SunSpec block (read by wallboxes and evcc) of a unit ID, each register with address, canonical field, type, scale factor, raw words and decoded value, from one consistent copy.
//	@Tags			meters
//	@Produce		json
//	@Security		ApiKeyAuth
//	@Param			unit	query		int					true	"Unit ID (1-247)"
//	@Success		200		{object}	meters.RegisterDump	"Registers of the unit ID"
//	@Failure		400		{object}	web.ApiError		"Invalid unit ID"
//	@Failure		401		{object}	web.ApiError		"Unauthorized"
//	@Failure		404		{object}	web.ApiError		"No meter answers on this unit ID"
//	@Router			/registers [get]
func (app *App) HandleRegisters() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unit, err := strconv.Atoi(r.URL.Query().Get("unit"))
		if err != nil || unit < 1 || unit > 247 {
			web.WriteError(w, r, http.StatusBadRequest, fmt.Errorf("unit must be a unit ID from 1 to 247, got %q", r.URL.Query().Get("unit")))
			return
		}
		if app.modbusServer == nil {
			web.WriteError(w, r, http.StatusNotFound, fmt.Errorf("%w %d", meters.ErrUnknownUnit, unit))
			return
		}

		dump, err := app.modbusServer.Registers(uint8(unit))
		if errors.Is(err, meters.ErrUnknownUnit) {
			web.WriteError(w, r, http.StatusNotFound, err)
			return
		}
		if err != nil {
			web.WriteError(w, r, http.StatusInternalServerError, err)
			return
		}
		web.Encode(w, http.StatusOK, dump)
	})
}
