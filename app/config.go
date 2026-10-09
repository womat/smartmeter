package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/womat/smartmeter/app/service/meters"
	"gopkg.in/yaml.v3"
)

const (
	ProdEnv = "prod"
	DevEnv  = "dev"
)

// Config holds the main application configuration.
type Config struct {
	Env            string                        `yaml:"env"`            // Application environment: dev | prod
	LogLevel       string                        `yaml:"logLevel"`       // Log level: debug | info | warning | error
	LogDestination string                        `yaml:"logDestination"` // Log output: stdout | stderr | /path/to/logfile
	Webserver      WebserverConfig               `yaml:"webserver"`      // Webserver configuration
	Listen         meters.ListenConfig           `yaml:"listen"`         // Modbus server facing inverter and clients
	Meter          map[string]meters.MeterConfig `yaml:"meter"`          // Emulated meters, keyed by name
}

// WebserverConfig holds HTTPS server settings.
type WebserverConfig struct {
	ListenHost string   `yaml:"listenHost"` // Host address for web server
	ListenPort int      `yaml:"listenPort"` // Port for web server
	ApiKey     string   `yaml:"apiKey"`     // API key for requests
	KeyFile    string   `yaml:"keyFile"`    // SSL private key file
	CertFile   string   `yaml:"certFile"`   // SSL certificate file
	BlockedIPs []string `yaml:"blockedIPs"` // Forbidden IP addresses or networks
	AllowedIPs []string `yaml:"allowedIPs"` // Allowed IP addresses or networks
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
		Meter: make(map[string]meters.MeterConfig),
	}
}

// envBraces matches ${VAR} references; see expandEnvBraces.
var envBraces = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnvBraces replaces ${VAR} with the value of the environment variable VAR, or with an
// empty string when it is unset. Unlike os.ExpandEnv it leaves every other "$" alone, so an API
// key or password containing "$" is not silently cut short.
func expandEnvBraces(s string) string {
	return envBraces.ReplaceAllStringFunc(s, func(ref string) string {
		return os.Getenv(envBraces.FindStringSubmatch(ref)[1])
	})
}

// LoadConfig loads configuration from a YAML file and expands ${VAR} environment references.
//
// Unknown keys are an error rather than ignored, so a misspelled or renamed key cannot silently
// leave its setting at the default.
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

	dec := yaml.NewDecoder(bytes.NewReader([]byte(expandEnvBraces(string(content)))))
	dec.KnownFields(true)
	if err = dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("failed to unmarshal config: %w%s", err, renamedKeyHint(err))
	}

	return cfg, nil
}

// renamedKeys explains keys of earlier releases, so an old config file fails with the fix
// instead of only "field ... not found".
var renamedKeys = map[string]string{
	"unitIDs": "renamed to unitIds",
	"unitID":  "renamed to unitId",
	"enabled": "removed from listen.tcp and listen.rtu: a listener is active when its block is present; delete or comment out the block to turn it off",
}

// unknownField matches the key in yaml.v3's "field X not found in type Y".
var unknownField = regexp.MustCompile(`field (\w+) not found`)

// renamedKeyHint returns "; <key>: <hint>" for every renamed key that err complains about.
func renamedKeyHint(err error) string {
	var hint strings.Builder
	seen := map[string]bool{}
	for _, m := range unknownField.FindAllStringSubmatch(err.Error(), -1) {
		if text, ok := renamedKeys[m[1]]; ok && !seen[m[1]] {
			seen[m[1]] = true
			fmt.Fprintf(&hint, "; %s: %s", m[1], text)
		}
	}
	return hint.String()
}

// Validate checks the Config for invalid or missing values and applies the per-meter defaults.
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

	c.Listen.ApplyDefaults()
	if c.Listen.TCP != nil && (c.Listen.TCP.Port < 1 || c.Listen.TCP.Port > 65535) {
		return fmt.Errorf("invalid listen.tcp.port: %d", c.Listen.TCP.Port)
	}
	if c.Listen.RTU != nil {
		if err := c.Listen.RTU.SerialConfig.Validate(); err != nil {
			return fmt.Errorf("invalid listen.rtu: %w", err)
		}
	}

	for _, name := range meters.Names(c.Meter) {
		meter := c.Meter[name]
		meter.Name = name
		meter.ApplyDefaults()
		if err := meter.Validate(); err != nil {
			return fmt.Errorf("invalid config for meter %q: %w", name, err)
		}
		c.Meter[name] = meter
	}

	return meters.CheckUniqueUnitIds(c.Meter)
}

// minApiKeyLength is the length below which Warnings flags the API key as weak.
const minApiKeyLength = 16

// Warnings returns findings that do not stop the service but should be fixed. It never includes
// secret values.
func (c *Config) Warnings() []string {
	var warnings []string

	key := c.Webserver.ApiKey
	switch {
	case strings.Contains(strings.ToLower(key), "changeme"):
		warnings = append(warnings, "apiKey is still the example value from the documentation; set a random key")
	case len(key) < minApiKeyLength:
		warnings = append(warnings, fmt.Sprintf("apiKey is shorter than %d characters; use a longer random key", minApiKeyLength))
	}

	return warnings
}
