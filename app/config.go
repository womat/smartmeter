package app

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ProdEnv = "prod"
	DevEnv  = "dev"
)

// Config holds the main application configuration.
type Config struct {
	Env            string          `yaml:"env"`            // Application environment: dev | prod
	LogLevel       string          `yaml:"logLevel"`       // Log level: debug | info | warning | error
	LogDestination string          `yaml:"logDestination"` // Log output: stdout | stderr | /path/to/logfile
	Webserver      WebserverConfig `yaml:"webserver"`      // Webserver configuration
	Listen         ListenConfig    `yaml:"listen"`         // Modbus listener configuration
	Devices        []DeviceConfig  `yaml:"devices"`        // Emulated smart meter devices
}

// WebserverConfig holds HTTPS server settings.
type WebserverConfig struct {
	ListenHost string   `yaml:"listenHost"` // Host address for web server
	ListenPort int      `yaml:"listenPort"` // Port for web server
	ApiKey     string   `yaml:"apiKey"`     // API key for requests
	JwtSecret  string   `yaml:"jwtSecret"`  // Secret for JWT tokens
	JwtID      string   `yaml:"jwtID"`      // Unique JWT ID
	KeyFile    string   `yaml:"keyFile"`    // SSL private key file
	CertFile   string   `yaml:"certFile"`   // SSL certificate file
	BlockedIPs []string `yaml:"blockedIPs"` // Forbidden IP addresses or networks
	AllowedIPs []string `yaml:"allowedIPs"` // Allowed IP addresses or networks
}

type ListenConfig struct {
	TCP ListenTCPConfig `yaml:"tcp"`
	RTU ListenRTUConfig `yaml:"rtu"`
}

type ListenTCPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

type ListenRTUConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Port     string `yaml:"port"`
	BaudRate int    `yaml:"baud_rate"`
	DataBits int    `yaml:"data_bits"`
	Parity   string `yaml:"parity"`
	StopBits int    `yaml:"stop_bits"`
}

type DeviceConfig struct {
	Name    string                   `yaml:"name"`
	UnitIDs []uint8                  `yaml:"unitIds"` // unit IDs the emulated meter answers on
	Source  SourceConfig             `yaml:"source"`
	Poll    PollConfig               `yaml:"poll"`
	Map     map[string]MappingConfig `yaml:"map"`
}

type SourceConfig struct {
	Type    string          `yaml:"type"`
	UnitID  uint8           `yaml:"unitId"`
	Timeout time.Duration   `yaml:"timeout"`
	TCP     TCPSourceConfig `yaml:"tcp"`
	RTU     RTUSourceConfig `yaml:"rtu"`
}

type TCPSourceConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type RTUSourceConfig struct {
	Port     string `yaml:"port"`
	BaudRate int    `yaml:"baud_rate"`
	DataBits int    `yaml:"data_bits"`
	Parity   string `yaml:"parity"`
	StopBits int    `yaml:"stop_bits"`
}

type PollConfig struct {
	Interval     time.Duration `yaml:"interval"`
	MaxBlockGap  int           `yaml:"max_block_gap"`
	MaxBlockSize int           `yaml:"max_block_size"`
}

type MappingConfig struct {
	Type      string  `yaml:"type"`
	Address   uint16  `yaml:"address"`
	DType     string  `yaml:"dtype"`
	ByteOrder string  `yaml:"byte_order"`
	WordOrder string  `yaml:"word_order"`
	Scale     int     `yaml:"scale"`
	Offset    float64 `yaml:"offset"`
	Value     any     `yaml:"value"`
	Expr      string  `yaml:"expr"`
}

// NewConfig returns a Config with sane defaults
func NewConfig() *Config {
	return &Config{
		Env:            DevEnv,
		LogLevel:       "info",
		LogDestination: "stdout",
		Webserver: WebserverConfig{
			ListenHost: "0.0.0.0",
			ListenPort: 8443,
			BlockedIPs: []string{},
			AllowedIPs: []string{},
		},
		Listen: ListenConfig{
			TCP: ListenTCPConfig{
				Host: "0.0.0.0",
				Port: 502,
			},
			RTU: ListenRTUConfig{
				BaudRate: 9600,
				DataBits: 8,
				Parity:   "N",
				StopBits: 1,
			},
		},
		Devices: []DeviceConfig{},
	}
}

// LoadConfig loads configuration from a YAML file and expands environment variables.
func LoadConfig(fileName string) (*Config, error) {
	cfg := NewConfig()

	fileInfo, err := os.Stat(fileName)
	if err != nil {
		return cfg, err
	}
	if fileInfo.IsDir() {
		return cfg, errors.New("config path is a directory, not a file")
	}

	content, err := os.ReadFile(fileName)
	if err != nil {
		return cfg, err
	}

	// Replace environment variables in the YAML
	replaced := os.ExpandEnv(string(content))

	// Unmarshal YAML into the config struct
	if err = yaml.Unmarshal([]byte(replaced), cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// IsDevEnv returns true if the environment is development.
func (c *Config) IsDevEnv() bool {
	return c.Env == DevEnv
}

// Validate checks the Config for invalid or missing values.
func (c *Config) Validate() error {

	if c.Env != ProdEnv && c.Env != DevEnv {
		return fmt.Errorf("invalid environment: %s, must be %s or %s", c.Env, ProdEnv, DevEnv)
	}

	if c.Webserver.ApiKey == "" {
		return errors.New("ApiKey is not configured")
	}

	validLogLevels := []string{"debug", "info", "warning", "warn", "error"}
	if !slices.Contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf("invalid log level: %s, must be one of %v", c.LogLevel, validLogLevels)
	}

	if c.Webserver.ListenPort < 1 || c.Webserver.ListenPort > 65535 {
		return fmt.Errorf("invalid port: %d", c.Webserver.ListenPort)
	}

	for i := range c.Devices {
		c.applyDeviceDefaults(&c.Devices[i])
		if err := c.validateDevice(c.Devices[i]); err != nil {
			return err
		}
	}

	return checkUniqueUnitIDs(c.Devices)
}

// checkUniqueUnitIDs ensures that every unit ID is served by exactly one device.
func checkUniqueUnitIDs(devices []DeviceConfig) error {
	owner := make(map[uint8]string)
	for _, device := range devices {
		for _, id := range device.UnitIDs {
			if name, exists := owner[id]; exists {
				return fmt.Errorf("unitId %d is used by device %q and device %q", id, name, device.Name)
			}
			owner[id] = device.Name
		}
	}
	return nil
}

func (c *Config) applyDeviceDefaults(device *DeviceConfig) {
	if device.Source.Timeout == 0 {
		device.Source.Timeout = 2 * time.Second
	}
	if device.Poll.Interval == 0 {
		device.Poll.Interval = time.Second
	}
	if device.Poll.MaxBlockGap == 0 {
		device.Poll.MaxBlockGap = 4
	}
	if device.Poll.MaxBlockSize == 0 {
		device.Poll.MaxBlockSize = 125
	}

	switch strings.ToLower(device.Source.Type) {
	case "tcp":
		if device.Source.TCP.Port == 0 {
			device.Source.TCP.Port = 502
		}
	case "rtu":
		if device.Source.RTU.BaudRate == 0 {
			device.Source.RTU.BaudRate = 9600
		}
		if device.Source.RTU.DataBits == 0 {
			device.Source.RTU.DataBits = 8
		}
		if device.Source.RTU.Parity == "" {
			device.Source.RTU.Parity = "N"
		}
		if device.Source.RTU.StopBits == 0 {
			device.Source.RTU.StopBits = 1
		}
	}

	for field, mapping := range device.Map {
		if mapping.ByteOrder == "" {
			mapping.ByteOrder = "big"
		}
		if mapping.WordOrder == "" {
			mapping.WordOrder = "big"
		}
		device.Map[field] = mapping
	}
}

func (c *Config) validateDevice(device DeviceConfig) error {
	if device.Name == "" {
		return errors.New("device name is required")
	}
	if len(device.UnitIDs) == 0 {
		return fmt.Errorf("device %q requires at least one entry in unitIds", device.Name)
	}
	for _, id := range device.UnitIDs {
		if id == 0 || id > 247 {
			return fmt.Errorf("device %q has invalid unitId %d", device.Name, id)
		}
	}
	if device.Source.UnitID == 0 || device.Source.UnitID > 247 {
		return fmt.Errorf("device %q has invalid source unitId %d", device.Name, device.Source.UnitID)
	}
	if device.Source.Timeout <= 0 {
		return fmt.Errorf("device %q has invalid source timeout %s", device.Name, device.Source.Timeout)
	}
	if device.Poll.Interval <= 0 {
		return fmt.Errorf("device %q has invalid poll interval %s", device.Name, device.Poll.Interval)
	}
	if device.Poll.MaxBlockGap < 0 {
		return fmt.Errorf("device %q has invalid max_block_gap %d", device.Name, device.Poll.MaxBlockGap)
	}
	if device.Poll.MaxBlockSize < 1 || device.Poll.MaxBlockSize > 125 {
		return fmt.Errorf("device %q has invalid max_block_size %d", device.Name, device.Poll.MaxBlockSize)
	}
	if len(device.Map) == 0 {
		return fmt.Errorf("device %q requires at least one map entry", device.Name)
	}

	switch strings.ToLower(device.Source.Type) {
	case "tcp":
		if device.Source.TCP.Host == "" {
			return fmt.Errorf("device %q source tcp.host is required", device.Name)
		}
		if device.Source.TCP.Port < 1 || device.Source.TCP.Port > 65535 {
			return fmt.Errorf("device %q source tcp.port is invalid", device.Name)
		}
	case "rtu":
		if device.Source.RTU.Port == "" {
			return fmt.Errorf("device %q source rtu.port is required", device.Name)
		}
		if device.Source.RTU.BaudRate <= 0 {
			return fmt.Errorf("device %q source rtu.baud_rate is invalid", device.Name)
		}
		if device.Source.RTU.DataBits < 5 || device.Source.RTU.DataBits > 8 {
			return fmt.Errorf("device %q source rtu.data_bits is invalid", device.Name)
		}
		if !slices.Contains([]string{"N", "E", "O"}, strings.ToUpper(device.Source.RTU.Parity)) {
			return fmt.Errorf("device %q source rtu.parity must be N, E or O", device.Name)
		}
		if device.Source.RTU.StopBits != 1 && device.Source.RTU.StopBits != 2 {
			return fmt.Errorf("device %q source rtu.stop_bits is invalid", device.Name)
		}
	default:
		return fmt.Errorf("device %q source type %q is invalid", device.Name, device.Source.Type)
	}

	validMappingTypes := []string{"register", "fixed", "expr"}
	validDTypes := []string{"uint16", "uint32", "uint64", "int16", "int32", "int64", "float32"}
	validByteOrders := []string{"big", "little"}

	for field, mapping := range device.Map {
		if !slices.Contains(validMappingTypes, strings.ToLower(mapping.Type)) {
			return fmt.Errorf("device %q field %q has invalid type %q", device.Name, field, mapping.Type)
		}

		switch strings.ToLower(mapping.Type) {
		case "register":
			if !slices.Contains(validDTypes, strings.ToLower(mapping.DType)) {
				return fmt.Errorf("device %q field %q has invalid dtype %q", device.Name, field, mapping.DType)
			}
			if !slices.Contains(validByteOrders, strings.ToLower(mapping.ByteOrder)) {
				return fmt.Errorf("device %q field %q has invalid byte_order %q", device.Name, field, mapping.ByteOrder)
			}
			if !slices.Contains(validByteOrders, strings.ToLower(mapping.WordOrder)) {
				return fmt.Errorf("device %q field %q has invalid word_order %q", device.Name, field, mapping.WordOrder)
			}
		case "fixed":
			if mapping.Value == nil {
				return fmt.Errorf("device %q field %q requires value for fixed mapping", device.Name, field)
			}
		case "expr":
			if strings.TrimSpace(mapping.Expr) == "" {
				return fmt.Errorf("device %q field %q requires expr for expression mapping", device.Name, field)
			}
		}
	}

	return nil
}
