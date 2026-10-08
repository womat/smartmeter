package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

func validTestMeter(unitIDs ...uint8) meters.MeterConfig {
	return meters.MeterConfig{
		UnitIDs: unitIDs,
		Source:  meters.SourceConfig{Type: "tcp", UnitID: 1, TCP: meters.TCPSourceConfig{Host: "127.0.0.1"}},
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
	if !slices.Equal(meter.UnitIDs, []uint8{1, 200}) {
		t.Errorf("UnitIDs = %v, want [1 200]", meter.UnitIDs)
	}
	if !cfg.Listen.RTU.Enabled || cfg.Listen.RTU.Port != "/dev/ttyS0" {
		t.Errorf("RTU listener = %+v, want enabled on /dev/ttyS0", cfg.Listen.RTU)
	}
	// One request for the whole Smartfox block, see TestSmartfoxSingleReadBlock.
	if meter.Poll.MaxBlockGap != 10 {
		t.Errorf("maxBlockGap = %d, want 10", meter.Poll.MaxBlockGap)
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	for name, content := range map[string]string{
		"old device list":   "devices:\n  - name: m\n",
		"old unitIds":       "meter:\n  m:\n    unitIds: [1]\n",
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
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, content))
			if err == nil || !strings.Contains(err.Error(), "time.Duration") {
				t.Errorf("err = %v, want a refused duration", err)
			}
		})
	}
}

func TestValidateUnitIDs(t *testing.T) {
	tests := []struct {
		name    string
		meters  map[string]meters.MeterConfig
		wantErr string // empty = valid
	}{
		{"single", map[string]meters.MeterConfig{"a": validTestMeter(200)}, ""},
		{"multiple", map[string]meters.MeterConfig{"a": validTestMeter(1, 200)}, ""},
		{"several meters", map[string]meters.MeterConfig{"a": validTestMeter(1, 200), "b": validTestMeter(201)}, ""},
		{"missing", map[string]meters.MeterConfig{"a": validTestMeter()}, "unitIDs requires at least one entry"},
		{"zero", map[string]meters.MeterConfig{"a": validTestMeter(0)}, "invalid unitID 0"},
		{"too large", map[string]meters.MeterConfig{"a": validTestMeter(248)}, "invalid unitID 248"},
		{"duplicate in meter", map[string]meters.MeterConfig{"a": validTestMeter(200, 200)}, `unitID 200 is used by meter "a" and meter "a"`},
		{"duplicate across meters", map[string]meters.MeterConfig{"a": validTestMeter(1, 200), "b": validTestMeter(200)}, `unitID 200 is used by meter "a" and meter "b"`},
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
