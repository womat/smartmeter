package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func validTestConfig(devices ...DeviceConfig) *Config {
	cfg := NewConfig()
	cfg.Webserver.ApiKey = "test"
	cfg.Devices = devices
	return cfg
}

func validTestDevice(name string, unitIDs ...uint8) DeviceConfig {
	return DeviceConfig{
		Name:    name,
		UnitIDs: unitIDs,
		Source:  SourceConfig{Type: "tcp", UnitID: 1, TCP: TCPSourceConfig{Host: "127.0.0.1"}},
		Map:     map[string]MappingConfig{"frequency": {Type: "fixed", Value: 50}},
	}
}

func TestLoadConfigUnitIDs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	content := `
webserver:
  apiKey: test
devices:
  - name: main_meter
    unitIds: [1, 200]
    source:
      type: tcp
      unitId: 1
      tcp:
        host: 127.0.0.1
    map:
      frequency:
        type: fixed
        value: 50
`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(file)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := cfg.Devices[0].UnitIDs; !slices.Equal(got, []uint8{1, 200}) {
		t.Fatalf("UnitIDs = %v, want [1 200]", got)
	}
}

func TestValidateUnitIDs(t *testing.T) {
	tests := []struct {
		name    string
		devices []DeviceConfig
		wantErr string // empty = valid
	}{
		{"single", []DeviceConfig{validTestDevice("a", 200)}, ""},
		{"multiple", []DeviceConfig{validTestDevice("a", 1, 200)}, ""},
		{"several devices", []DeviceConfig{validTestDevice("a", 1, 200), validTestDevice("b", 201)}, ""},
		{"missing", []DeviceConfig{validTestDevice("a")}, "at least one entry in unitIds"},
		{"zero", []DeviceConfig{validTestDevice("a", 0)}, "invalid unitId 0"},
		{"too large", []DeviceConfig{validTestDevice("a", 248)}, "invalid unitId 248"},
		{"duplicate in device", []DeviceConfig{validTestDevice("a", 200, 200)}, "unitId 200 is used by"},
		{"duplicate across devices", []DeviceConfig{validTestDevice("a", 1, 200), validTestDevice("b", 200)}, "unitId 200 is used by"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validTestConfig(tc.devices...).Validate()
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
	device := validTestDevice("a", 1)
	device.MaxCurrent = -1
	if err := validTestConfig(device).Validate(); err == nil || !strings.Contains(err.Error(), "max_current") {
		t.Fatalf("Validate() error = %v, want max_current error", err)
	}
}

// TestRepoConfigs keeps the shipped configuration files loadable and valid.
func TestRepoConfigs(t *testing.T) {
	t.Setenv("SMARTMETER_API_KEY", "test")

	for _, name := range []string{"config.yaml", "primary-meter.yaml"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadConfig(filepath.Join("..", "config", name))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if err = cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if _, err = NewModbusService(cfg); err != nil {
				t.Fatalf("NewModbusService() error = %v", err)
			}
		})
	}
}

func TestPrimaryMeterConfig(t *testing.T) {
	t.Setenv("SMARTMETER_API_KEY", "test")

	cfg, err := LoadConfig(filepath.Join("..", "config", "primary-meter.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	device := cfg.Devices[0]
	if !slices.Equal(device.UnitIDs, []uint8{1, 200}) {
		t.Errorf("UnitIDs = %v, want [1 200]", device.UnitIDs)
	}
	if !cfg.Listen.RTU.Enabled || cfg.Listen.RTU.Port != "/dev/ttyS0" {
		t.Errorf("RTU listener = %+v, want enabled on /dev/ttyS0", cfg.Listen.RTU)
	}

	// The Smartfox block is read with a single request, like the old emulator.
	compiled, err := compileDevice(device)
	if err != nil {
		t.Fatalf("compileDevice() error = %v", err)
	}
	if want := []readBlock{{Start: 40999, Quantity: 39}}; !slices.Equal(compiled.Blocks, want) {
		t.Errorf("Blocks = %v, want %v", compiled.Blocks, want)
	}
}
