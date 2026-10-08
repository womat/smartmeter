package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/womat/golib/web"
	"github.com/womat/smartmeter/app/service/health"
	"github.com/womat/smartmeter/app/service/meters"
)

// newTestApp returns an App with routes set up and the given meters compiled but not polled.
func newTestApp(t *testing.T, meterConfigs map[string]meters.MeterConfig) *App {
	t.Helper()
	cfg := validTestConfig(meterConfigs)
	cfg.Webserver.ApiKey = "test-key"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	client, err := meters.NewModbusService(cfg.Meter)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{config: cfg, web: &http.Server{}, modbusClient: client}
	app.SetupRoutes()
	return app
}

func serve(app *App, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	rec := httptest.NewRecorder()
	app.web.Handler.ServeHTTP(rec, req)
	return rec
}

func TestPublicAndProtectedRoutes(t *testing.T) {
	app := newTestApp(t, nil)

	for _, tc := range []struct {
		path, key string
		code      int
	}{
		{"/version", "", http.StatusOK},
		{"/ready", "", http.StatusOK}, // no meters: nothing can be outdated
		{"/health", "", http.StatusUnauthorized},
		{"/health", "test-key", http.StatusOK},
	} {
		if rec := serve(app, tc.path, tc.key); rec.Code != tc.code {
			t.Errorf("GET %s (key %q) = %d, want %d", tc.path, tc.key, rec.Code, tc.code)
		}
	}
}

func TestReadyAndHealthReportMeterState(t *testing.T) {
	app := newTestApp(t, map[string]meters.MeterConfig{"primary_meter": validTestMeter(1, 200)})

	// Compiled but never polled: the meter has no current values.
	rec := serve(app, "/ready", "")
	var apiErr web.ApiError
	if rec.Code != http.StatusServiceUnavailable || json.Unmarshal(rec.Body.Bytes(), &apiErr) != nil || apiErr.Error == "" {
		t.Errorf("GET /ready = %d %s, want 503 with a reason", rec.Code, rec.Body)
	}

	rec = serve(app, "/health", "test-key")
	var got health.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET /health body = %s: %v", rec.Body, err)
	}
	st, ok := got.Meters["primary_meter"]
	if !ok || st.Ready || len(st.UnitIDs) != 2 {
		t.Errorf("GET /health meters = %+v, want primary_meter not ready on 2 unit IDs", got.Meters)
	}
}
