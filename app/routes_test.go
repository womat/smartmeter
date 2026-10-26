package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/womat/golib/v2/web"
	"github.com/womat/smartmeter/app/service/health"
	"github.com/womat/smartmeter/app/service/meters"
)

// newTestApp returns an App with routes set up and the given meters compiled but not polled.
func newTestApp(t *testing.T, meterConfigs map[string]meters.MeterConfig) *App {
	t.Helper()
	cfg := validTestConfig(meterConfigs)
	cfg.Webserver.Tokens = []web.Token{{Name: "test", Hash: web.HashToken("test-key")}}
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

// serve sends a GET for path, with token as bearer token unless it is empty.
func serve(app *App, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
		{"/health", "", http.StatusUnauthorized},
		{"/health", "test-key", http.StatusOK},
	} {
		if rec := serve(app, tc.path, tc.key); rec.Code != tc.code {
			t.Errorf("GET %s (key %q) = %d, want %d", tc.path, tc.key, rec.Code, tc.code)
		}
	}
}

func TestHealthReportsMeterState(t *testing.T) {
	app := newTestApp(t, map[string]meters.MeterConfig{"primary_meter": validTestMeter(1, 200)})

	// Compiled but never polled: the meter has no current values.
	rec := serve(app, "/health", "test-key")
	if !strings.Contains(rec.Body.String(), `"unitIds":[1,200]`) {
		t.Errorf("GET /health body = %s, want unitIds as a JSON array of numbers", rec.Body)
	}
	var got health.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET /health body = %s: %v", rec.Body, err)
	}
	st, ok := got.Meters["primary_meter"]
	if !ok || st.Ready || len(st.UnitIds) != 2 {
		t.Errorf("GET /health meters = %+v, want primary_meter not ready on 2 unit IDs", got.Meters)
	}
}

func TestWebPage(t *testing.T) {
	app := newTestApp(t, nil)

	rec := serve(app, "/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>smartmeter</title>") {
		t.Fatalf("GET / = %d, want the web page without a token", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, want requests limited to this server", csp)
	}
	// No "OPTIONS /" route (WithCORS answers preflights), so an unknown path is a 404, not 405.
	if rec = serve(app, "/nonexistent", ""); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("GET /nonexistent = %d, want 404 without the page: the page must not catch unknown paths", rec.Code)
	}
}

// The API is read-only: a preflight needs no token and advertises GET and OPTIONS only.
func TestCORSAdvertisesUsedMethodsOnly(t *testing.T) {
	app := newTestApp(t, nil)

	// A preflight is an OPTIONS request with Access-Control-Request-Method; WithCORS answers it
	// before auth and the mux.
	req := httptest.NewRequest(http.MethodOptions, "/registers", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	app.web.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight OPTIONS /registers = %d, want 204 without a token", rec.Code)
	}
	if got, want := rec.Header().Get("Access-Control-Allow-Methods"), "GET, OPTIONS"; got != want {
		t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, want)
	}
}

func TestRegistersRoute(t *testing.T) {
	cfg := validTestConfig(map[string]meters.MeterConfig{"primary_meter": validTestMeter(1, 200)})
	cfg.Webserver.Tokens = []web.Token{{Name: "test", Hash: web.HashToken("test-key")}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	server, err := meters.NewModbusServerService(cfg.Meter)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{config: cfg, web: &http.Server{}, modbusServer: server}
	app.SetupRoutes()

	for _, tc := range []struct {
		path, key string
		code      int
	}{
		{"/registers?unit=1", "", http.StatusUnauthorized},
		{"/registers?unit=1", "test-key", http.StatusOK},
		{"/registers?unit=200", "test-key", http.StatusOK},
		{"/registers?unit=9", "test-key", http.StatusNotFound},
		{"/registers?unit=0", "test-key", http.StatusBadRequest},
		{"/registers?unit=x", "test-key", http.StatusBadRequest},
		{"/registers", "test-key", http.StatusBadRequest},
	} {
		if rec := serve(app, tc.path, tc.key); rec.Code != tc.code {
			t.Errorf("GET %s (key %q) = %d, want %d: %s", tc.path, tc.key, rec.Code, tc.code, rec.Body)
		}
	}

	var dump meters.RegisterDump
	rec := serve(app, "/registers?unit=200", "test-key")
	if err = json.Unmarshal(rec.Body.Bytes(), &dump); err != nil {
		t.Fatal(err)
	}
	// Silent until the first snapshot, as NewModbusServerService leaves it.
	if dump.UnitId != 200 || dump.Online || len(dump.Proprietary) == 0 || len(dump.SunSpec) == 0 {
		t.Errorf("GET /registers?unit=200 = unit %d online %v, %d/%d rows", dump.UnitId, dump.Online, len(dump.Proprietary), len(dump.SunSpec))
	}
}

// The page hides the banner and other elements with the hidden attribute; a class that sets
// display would override it without this rule.
func TestWebPageHidesHiddenElements(t *testing.T) {
	if !strings.Contains(string(uiPage), "[hidden] { display: none !important; }") {
		t.Error("app/ui/index.html lacks the [hidden] rule: an empty banner would show")
	}
}
