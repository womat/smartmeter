//go:build swagger

package app

import (
	"net/http"

	_ "github.com/womat/smartmeter/docs"

	httpSwagger "github.com/swaggo/http-swagger"
)

// PathSwagger is where the Swagger UI is served in builds with the swagger tag.
const PathSwagger = "/swagger/"

func (app *App) registerSwaggerRoute(mux *http.ServeMux) {
	mux.Handle("GET "+PathSwagger, httpSwagger.Handler(
		httpSwagger.PersistAuthorization(true),
	))
}
