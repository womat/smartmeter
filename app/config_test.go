package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/womat/smartmeter/app/service/meters"
)

// writeConfig writes content to a temporary config file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func validTestConfig(meters map[string]meters.MeterConfig) *Config {
	cfg := NewConfig()
	cfg.Webserver.ApiKey = "test"
	cfg.Meter = meters
	return cfg
}

func validTestMeter(unitIds ...uint8) meters.MeterConfig {
	return meters.MeterConfig{
		UnitIds: unitIds,
		Source:  meters.SourceConfig{Type: "tcp", UnitId: 1, TCP: meters.TCPSourceConfig{Host: "127.0.0.1"}},
		Map:     map[string]meters.MappingConfig{"frequency": {Type: "fixed", Value: 50}},
	}
}

func TestLoadConfigExample(t *testing.T) {
	cfg, err := LoadConfig("../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatalf("the shipped example config does not validate: %v", err)
	}
	if _, err = meters.NewModbusService(cfg.Meter); err != nil {
		t.Fatalf("NewModbusService() error = %v", err)
	}

	meter, ok := cfg.Meter["primary_meter"]
	if !ok {
		t.Fatalf("the example config defines no meter primary_meter, got %v", meters.Names(cfg.Meter))
	}
	if meter.Name != "primary_meter" {
		t.Errorf("Name = %q, want the map key", meter.Name)
	}
	if !slices.Equal(meter.UnitIds, []uint8{1, 200}) {
		t.Errorf("UnitIds = %v, want [1 200]", meter.UnitIds)
	}
	if cfg.Listen.RTU == nil || cfg.Listen.RTU.Port != "/dev/ttyS0" {
		t.Errorf("RTU listener = %+v, want one on /dev/ttyS0", cfg.Listen.RTU)
	}
	// One request for the whole Smartfox block, see TestSmartfoxSingleReadBlock.
	if meter.Poll.MaxBlockGap != 10 {
		t.Errorf("maxBlockGap = %d, want 10", meter.Poll.MaxBlockGap)
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	for name, content := range map[string]string{
		"old device list":   "devices:\n  - name: m\n",
		"old unitIDs":       "meter:\n  m:\n    unitIDs: [1]\n",
		"old snake_case":    "listen:\n  rtu:\n    baud_rate: 9600\n",
		"removed jwtSecret": "webserver:\n  jwtSecret: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, content)); err == nil {
				t.Error("expected an error for the unknown key")
			}
		})
	}
}

func TestLoadConfigExplainsRenamedKeys(t *testing.T) {
	for name, tc := range map[string]struct{ content, want string }{
		"unitIDs":         {"meter:\n  m:\n    unitIDs: [1]\n", "unitIDs: renamed to unitIds"},
		"source.unitID":   {"meter:\n  m:\n    source:\n      unitID: 1\n", "unitID: renamed to unitId"},
		"listen.enabled":  {"listen:\n  tcp:\n    enabled: true\n", "enabled: removed from listen.tcp and listen.rtu"},
		"rtu.enabled off": {"listen:\n  rtu:\n    enabled: false\n    port: /dev/ttyS0\n", "delete or comment out the block"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestListenBlockPresenceAndDefaults(t *testing.T) {
	base := "webserver:\n  apiKey: 0123456789abcdef\n"

	cfg, err := LoadConfig(writeConfig(t, base))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.TCP != nil || cfg.Listen.RTU != nil {
		t.Errorf("listen = %+v without listen blocks, want no listener", cfg.Listen)
	}

	cfg, err = LoadConfig(writeConfig(t, base+"listen:\n  tcp: {}\n  rtu:\n    port: /dev/ttyS0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if tcp := cfg.Listen.TCP; tcp == nil || tcp.Host != "0.0.0.0" || tcp.Port != 502 {
		t.Errorf("listen.tcp = %+v, want the defaults 0.0.0.0:502", tcp)
	}
	want := meters.SerialConfig{Port: "/dev/ttyS0", BaudRate: 9600, DataBits: 8, Parity: "N", StopBits: 1}
	if rtu := cfg.Listen.RTU; rtu == nil || rtu.SerialConfig != want {
		t.Errorf("listen.rtu = %+v, want %+v", rtu, want)
	}

	// A present block must be complete: a forgotten port is an error, not a listener that is off.
	cfg, err = LoadConfig(writeConfig(t, base+"listen:\n  rtu:\n    baudRate: 19200\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Validate(); err == nil || !strings.Contains(err.Error(), "port is required") {
		t.Errorf("Validate() = %v for listen.rtu without port, want \"port is required\"", err)
	}
}

func TestLoadConfigEmptyFileGivesDefaults(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webserver.ListenPort != NewConfig().Webserver.ListenPort {
		t.Errorf("listenPort = %d, want the default", cfg.Webserver.ListenPort)
	}
}

func TestLoadConfigExpandsBracedVariablesOnly(t *testing.T) {
	t.Setenv("SMARTMETER_TEST_KEY", "from-env")
	cfg, err := LoadConfig(writeConfig(t, "webserver:\n  apiKey: ${SMARTMETER_TEST_KEY}\n  keyFile: /tmp/pa$word\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webserver.ApiKey != "from-env" {
		t.Errorf("apiKey = %q, want the environment value", cfg.Webserver.ApiKey)
	}
	if cfg.Webserver.KeyFile != "/tmp/pa$word" {
		t.Errorf("keyFile = %q, a bare $ must be kept", cfg.Webserver.KeyFile)
	}
}

// yaml.v3 refuses a duration without a unit; this pins that, so a bare number can never be
// read as nanoseconds.
func TestLoadConfigRejectsDurationWithoutUnit(t *testing.T) {
	for name, content := range map[string]string{
		"poll interval":  "meter:\n  m:\n    poll:\n      interval: 1\n",
		"source timeout": "meter:\n  m:\n    source:\n      timeout: 300\n",
		"frame delay":    "listen:\n  rtu:\n    interFrameDelay: 20\n",
		"stale timeout":  "meter:\n  m:\n    staleTimeout: 30\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, content))
			if err == nil || !strings.Contains(err.Error(), "time.Duration") {
				t.Errorf("err = %v, want a refused duration", err)
			}
		})
	}
}

func TestLoadConfigStaleTimeoutAndLimits(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "meter:\n  a:\n    staleTimeout: 0s\n    limits:\n      importPower: 15000\n      exportPower: 4500\n      current: 20\n  b:\n    unitIds: [2]\n"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := cfg.Meter["a"], cfg.Meter["b"]
	if a.StaleTimeout == nil || *a.StaleTimeout != 0 || a.Stale() != 0 {
		t.Errorf("staleTimeout 0s = %v, want an explicit 0 (never)", a.StaleTimeout)
	}
	if b.StaleTimeout != nil || b.Stale() != 30*time.Second {
		t.Errorf("staleTimeout unset = %v, want nil and the 30 s default", b.StaleTimeout)
	}
	if a.Limits != (meters.LimitsConfig{ImportPower: 15000, ExportPower: 4500, Current: 20}) {
		t.Errorf("limits = %+v", a.Limits)
	}
}

func TestValidateUnitIds(t *testing.T) {
	tests := []struct {
		name    string
		meters  map[string]meters.MeterConfig
		wantErr string // empty = valid
	}{
		{"single", map[string]meters.MeterConfig{"a": validTestMeter(200)}, ""},
		{"multiple", map[string]meters.MeterConfig{"a": validTestMeter(1, 200)}, ""},
		{"several meters", map[string]meters.MeterConfig{"a": validTestMeter(1, 200), "b": validTestMeter(201)}, ""},
		{"missing", map[string]meters.MeterConfig{"a": validTestMeter()}, "unitIds requires at least one entry"},
		{"zero", map[string]meters.MeterConfig{"a": validTestMeter(0)}, "invalid unitId 0"},
		{"too large", map[string]meters.MeterConfig{"a": validTestMeter(248)}, "invalid unitId 248"},
		{"duplicate in meter", map[string]meters.MeterConfig{"a": validTestMeter(200, 200)}, `unitId 200 is used by meter "a" and meter "a"`},
		{"duplicate across meters", map[string]meters.MeterConfig{"a": validTestMeter(1, 200), "b": validTestMeter(200)}, `unitId 200 is used by meter "a" and meter "b"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validTestConfig(tc.meters).Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Validate() error = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateMaxCurrent(t *testing.T) {
	meter := validTestMeter(1)
	meter.MaxCurrent = -1
	err := validTestConfig(map[string]meters.MeterConfig{"a": meter}).Validate()
	if err == nil || !strings.Contains(err.Error(), "maxCurrent") {
		t.Fatalf("Validate() error = %v, want maxCurrent error", err)
	}
}

func TestWarningsWeakApiKey(t *testing.T) {
	for key, want := range map[string]bool{
		"changeme!":                  true,
		"short":                      true,
		"a-long-random-api-key-1234": false,
	} {
		cfg := NewConfig()
		cfg.Webserver.ApiKey = key
		if got := len(cfg.Warnings()) > 0; got != want {
			t.Errorf("apiKey %q: warning = %v, want %v", key, got, want)
		}
	}
}
